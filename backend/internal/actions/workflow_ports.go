package actions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

var (
	ErrWorkflowUnavailable = errors.New("connector action workflow is unavailable")
	ErrMutationUnchanged   = errors.New("connector action mutation is unchanged")
	ErrTokenNotFound       = errors.New("connector action token not found")
)

type AuthorizationToken struct {
	ID        int64
	RevokedAt string
	ExpiresAt string
	Active    bool
}

type TokenReader interface {
	Get(context.Context, int64, time.Time) (AuthorizationToken, error)
}

type DeliveryGate interface {
	Acquire(context.Context) (func(), error)
}

type SealedRecords interface {
	SealActionRequest(int64, ExecutionEnvelope) (string, error)
	OpenActionRequest(int64, string) (ExecutionEnvelope, error)
	OpenCredentialProfile(int64, string) (map[string]any, error)
}

type AuditAppender func(*sql.Tx, string, *int64, int64, string, any) error

type MutationPort interface {
	WithMutation(context.Context, string, *int64, int64, string, func() any, func(*sql.Tx) error) error
	WithTransaction(context.Context, func(*sql.Tx, AuditAppender) error) error
	Observe(context.Context, string, *int64, int64, string, any)
}

type RuntimeIdentity func() (workspaceID string, runtimeInstanceID string, err error)
type IdentityTagger func([]byte) (string, error)
type CapabilityProvider func(string, []ResolvedDependency) connectors.RuntimeCapabilityResolver
type RunningActions interface {
	SupportsRunning(PreparedRequest) bool
	FinishRunning(context.Context, int64, PreparedRequest, executionprincipal.Principal, connectors.ActionHandles)
}

type RuntimeDependencies struct {
	Database       *sql.DB
	Tokens         TokenReader
	Registry       connectors.Catalog
	Targets        TargetResolver
	IdentityTag    IdentityTagger
	Delivery       DeliveryGate
	MCPStarted     func() bool
	Identity       RuntimeIdentity
	Redactor       *actionresult.Redactor
	SealedRecords  SealedRecords
	Mutations      MutationPort
	Capabilities   CapabilityProvider
	RunningActions RunningActions
	Now            func() time.Time
	Logf           func(string, ...any)
}

// Runtime owns one unlocked workspace's connector action lifecycle. Its ports
// deliberately exclude HTTP and gateway composition types.
type Runtime struct {
	database       *sql.DB
	tokens         TokenReader
	service        *Service
	identityTag    IdentityTagger
	delivery       DeliveryGate
	mcpStarted     func() bool
	identity       RuntimeIdentity
	redactor       *actionresult.Redactor
	sealedRecords  SealedRecords
	mutations      MutationPort
	capabilities   CapabilityProvider
	runningActions RunningActions
	now            func() time.Time
	logf           func(string, ...any)

	boundaryMu      sync.RWMutex
	boundaries      map[int64]actionresult.CredentialBoundary
	recoveryMu      sync.Mutex
	recoveryCancel  context.CancelFunc
	recoveryDone    chan struct{}
	finalizerMu     sync.Mutex
	finalizerCtx    context.Context
	finalizerCancel context.CancelFunc
	finalizerWG     sync.WaitGroup
	finalizerDone   chan struct{}
	finalizerWait   sync.Once
	finalizerClosed bool
}

func NewRuntime(dependencies RuntimeDependencies) (*Runtime, error) {
	if dependencies.Database == nil || dependencies.Tokens == nil || dependencies.Registry == nil || dependencies.Targets == nil ||
		dependencies.IdentityTag == nil ||
		dependencies.Delivery == nil || dependencies.MCPStarted == nil ||
		dependencies.Identity == nil || dependencies.Redactor == nil || dependencies.SealedRecords == nil ||
		dependencies.Mutations == nil || dependencies.Capabilities == nil || dependencies.RunningActions == nil {
		return nil, ErrWorkflowUnavailable
	}
	now := dependencies.Now
	if now == nil {
		now = time.Now
	}
	logf := dependencies.Logf
	if logf == nil {
		logf = log.Printf
	}
	finalizerCtx, finalizerCancel := context.WithCancel(context.Background())
	return &Runtime{
		database: dependencies.Database, tokens: dependencies.Tokens,
		service: NewService(dependencies.Registry, dependencies.Targets), identityTag: dependencies.IdentityTag,
		delivery: dependencies.Delivery, mcpStarted: dependencies.MCPStarted, identity: dependencies.Identity,
		redactor: dependencies.Redactor, sealedRecords: dependencies.SealedRecords, mutations: dependencies.Mutations,
		capabilities: dependencies.Capabilities, runningActions: dependencies.RunningActions, now: now, logf: logf,
		boundaries:   make(map[int64]actionresult.CredentialBoundary),
		finalizerCtx: finalizerCtx, finalizerCancel: finalizerCancel,
		finalizerDone: make(chan struct{}),
	}, nil
}

func (r *Runtime) launchFinalizer(run func(context.Context)) bool {
	if r == nil || run == nil {
		return false
	}
	r.finalizerMu.Lock()
	if r.finalizerClosed || r.finalizerCtx == nil {
		r.finalizerMu.Unlock()
		return false
	}
	ctx := r.finalizerCtx
	r.finalizerWG.Add(1)
	r.finalizerMu.Unlock()
	go func() {
		defer r.finalizerWG.Done()
		run(ctx)
	}()
	return true
}

// BeginFinalizerShutdown prevents new background completions and cancels
// active ones without waiting for their persistence work to return.
func (r *Runtime) BeginFinalizerShutdown() {
	if r == nil {
		return
	}
	r.finalizerMu.Lock()
	r.finalizerClosed = true
	cancel := r.finalizerCancel
	r.finalizerCancel = nil
	r.finalizerMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// WaitFinalizers waits until finalizers can no longer access workspace-owned
// storage. Repeated waits observe the same drain signal.
func (r *Runtime) WaitFinalizers(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.finalizerWait.Do(func() {
		go func() {
			r.finalizerWG.Wait()
			close(r.finalizerDone)
		}()
	})
	select {
	case <-r.finalizerDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// StopFinalizers closes admission, cancels active finalizers, and waits for
// the runtime to drain.
func (r *Runtime) StopFinalizers(ctx context.Context) error {
	r.BeginFinalizerShutdown()
	return r.WaitFinalizers(ctx)
}

func (r *Runtime) validate() error {
	if r == nil || r.database == nil || r.tokens == nil || r.service == nil ||
		r.identityTag == nil || r.delivery == nil || r.mcpStarted == nil || r.identity == nil || r.redactor == nil ||
		r.sealedRecords == nil || r.mutations == nil || r.capabilities == nil || r.runningActions == nil || r.now == nil {
		return ErrWorkflowUnavailable
	}
	return nil
}

func (r *Runtime) runtimeIdentity() (string, string, error) {
	if err := r.validate(); err != nil {
		return "", "", err
	}
	workspaceID, runtimeInstanceID, err := r.identity()
	if err != nil {
		return "", "", err
	}
	if workspaceID == "" || runtimeInstanceID == "" {
		return "", "", executionprincipal.ErrInvalid
	}
	return workspaceID, runtimeInstanceID, nil
}

func (r *Runtime) TrackCredentialBoundary(requestID int64, boundary actionresult.CredentialBoundary) {
	if r == nil || requestID < 1 {
		return
	}
	r.boundaryMu.Lock()
	if r.boundaries == nil {
		r.boundaries = make(map[int64]actionresult.CredentialBoundary)
	}
	r.boundaries[requestID] = boundary
	r.boundaryMu.Unlock()
}

func (r *Runtime) CredentialBoundary(requestID int64) (actionresult.CredentialBoundary, bool) {
	if r == nil || requestID < 1 {
		return actionresult.CredentialBoundary{}, false
	}
	r.boundaryMu.RLock()
	boundary, ok := r.boundaries[requestID]
	r.boundaryMu.RUnlock()
	return boundary, ok
}

func (r *Runtime) ClearCredentialBoundary(requestID int64) {
	if r == nil || requestID < 1 {
		return
	}
	r.boundaryMu.Lock()
	delete(r.boundaries, requestID)
	r.boundaryMu.Unlock()
}

func (r *Runtime) CredentialBoundaryForRequest(ctx context.Context, requestID int64) (actionresult.CredentialBoundary, error) {
	if err := r.validate(); err != nil {
		return actionresult.CredentialBoundary{}, err
	}
	if boundary, ok := r.CredentialBoundary(requestID); ok {
		return boundary, nil
	}
	store := connectortargets.NewStore(r.database)
	request, err := store.GetActionRequest(ctx, requestID)
	if err != nil {
		return actionresult.CredentialBoundary{}, err
	}
	profile, err := store.GetCredentialProfile(ctx, request.TargetID, request.ProfileID)
	if err != nil {
		return actionresult.CredentialBoundary{}, err
	}
	secrets, err := r.sealedRecords.OpenCredentialProfile(profile.ID, profile.EncryptedSecretJSON)
	if err != nil {
		return actionresult.CredentialBoundary{}, err
	}
	boundary := actionresult.NewCredentialBoundary(secrets)
	if request.EncryptedPayloadJSON == "" {
		return boundary, nil
	}
	envelope, err := r.sealedRecords.OpenActionRequest(request.ID, request.EncryptedPayloadJSON)
	if err != nil {
		return actionresult.CredentialBoundary{}, fmt.Errorf("decrypt connector action redaction boundary: %w", err)
	}
	boundary.Add(actionresult.SensitiveValues(envelope.Input, envelope.Payload, envelope.SensitiveInputFields)...)
	boundary.Add(actionresult.SensitiveValues(envelope.ApprovalPreview, nil, envelope.SensitiveInputFields)...)
	return boundary, nil
}
