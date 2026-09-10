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
type CapabilityProvider func(string, []ResolvedDependency) connectors.RuntimeCapabilityResolver
type UserNoteEnqueuer func(context.Context, *sql.Tx, int64, string) error

type RunningActions interface {
	SupportsRunning(PreparedRequest) bool
	FinishRunning(int64, PreparedRequest, executionprincipal.Principal, connectors.ActionHandles)
}

type RuntimeDependencies struct {
	Database        *sql.DB
	Tokens          TokenReader
	Registry        *connectors.Registry
	Targets         TargetResolver
	IdentityKey     []byte
	Delivery        DeliveryGate
	MCPStarted      func() bool
	Identity        RuntimeIdentity
	Redactor        *actionresult.Redactor
	SealedRecords   SealedRecords
	Mutations       MutationPort
	Capabilities    CapabilityProvider
	RunningActions  RunningActions
	EnqueueUserNote UserNoteEnqueuer
	Now             func() time.Time
	Logf            func(string, ...any)
}

// Runtime owns one unlocked workspace's connector action lifecycle. Its ports
// deliberately exclude HTTP and gateway composition types.
type Runtime struct {
	database        *sql.DB
	tokens          TokenReader
	service         *Service
	identityKey     []byte
	delivery        DeliveryGate
	mcpStarted      func() bool
	identity        RuntimeIdentity
	redactor        *actionresult.Redactor
	sealedRecords   SealedRecords
	mutations       MutationPort
	capabilities    CapabilityProvider
	runningActions  RunningActions
	enqueueUserNote UserNoteEnqueuer
	now             func() time.Time
	logf            func(string, ...any)

	boundaryMu     sync.RWMutex
	boundaries     map[int64]actionresult.CredentialBoundary
	recoveryMu     sync.Mutex
	recoveryCancel context.CancelFunc
	recoveryDone   chan struct{}
}

func NewRuntime(dependencies RuntimeDependencies) (*Runtime, error) {
	if dependencies.Database == nil || dependencies.Tokens == nil || dependencies.Registry == nil || dependencies.Targets == nil ||
		len(dependencies.IdentityKey) != 32 ||
		dependencies.Delivery == nil || dependencies.MCPStarted == nil ||
		dependencies.Identity == nil || dependencies.Redactor == nil || dependencies.SealedRecords == nil ||
		dependencies.Mutations == nil || dependencies.Capabilities == nil || dependencies.RunningActions == nil ||
		dependencies.EnqueueUserNote == nil {
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
	return &Runtime{
		database: dependencies.Database, tokens: dependencies.Tokens,
		service: NewService(dependencies.Registry, dependencies.Targets), identityKey: dependencies.IdentityKey,
		delivery: dependencies.Delivery, mcpStarted: dependencies.MCPStarted, identity: dependencies.Identity,
		redactor: dependencies.Redactor, sealedRecords: dependencies.SealedRecords, mutations: dependencies.Mutations,
		capabilities: dependencies.Capabilities, runningActions: dependencies.RunningActions,
		enqueueUserNote: dependencies.EnqueueUserNote, now: now, logf: logf,
		boundaries: make(map[int64]actionresult.CredentialBoundary),
	}, nil
}

func (r *Runtime) validate() error {
	if r == nil || r.database == nil || r.tokens == nil || r.service == nil ||
		r.delivery == nil || r.mcpStarted == nil || r.identity == nil || r.redactor == nil ||
		r.sealedRecords == nil || r.mutations == nil || r.capabilities == nil || r.runningActions == nil ||
		r.enqueueUserNote == nil || r.now == nil {
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
