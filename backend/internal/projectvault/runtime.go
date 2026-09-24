package projectvault

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrRuntimeUnavailable = errors.New("Project Vault runtime is unavailable")
var ErrGenerateRateLimited = errors.New("too many generated previews; wait before trying again")
var ErrRevealRateLimited = errors.New("too many reveal requests; wait before trying again")
var ErrBindingTargetNotFound = errors.New("connector target not found")
var ErrSessionEnvironmentUnsupported = errors.New("connector profile does not support Vault session environments")

type DeliveryGate interface {
	AcquireDelivery(context.Context) (func(), error)
	AcquireExclusive(context.Context) (func(), error)
}

type MutationPort interface {
	WithMutation(context.Context, string, func() any, func(*sql.Tx) error) error
	Observe(context.Context, string, any) error
}

type SessionInvalidator func(context.Context, []SessionReference, SessionMutationScope) error
type RateLimiter func(string) bool

type BindingTargetValidator interface {
	ValidateDefaultBindingTarget(context.Context, int64, int64) error
}

type RuntimeDependencies struct {
	Store              *Store
	Delivery           DeliveryGate
	Mutations          MutationPort
	InvalidateSessions SessionInvalidator
	BindingTargets     BindingTargetValidator
	AllowGenerate      RateLimiter
	AllowReveal        RateLimiter
	Now                func() time.Time
	Nonce              func() (string, error)
}

// Runtime owns local-operator Project Vault item workflows. Transport adapters
// in this package expose those workflows without leaking persistence details.
type Runtime struct {
	store              *Store
	delivery           DeliveryGate
	mutations          MutationPort
	invalidateSessions SessionInvalidator
	bindingTargets     BindingTargetValidator
	allowGenerate      RateLimiter
	allowReveal        RateLimiter
	now                func() time.Time
	nonce              func() (string, error)

	previewMu     sync.Mutex
	previewNonces map[int64]string
}

func NewRuntime(dependencies RuntimeDependencies) (*Runtime, error) {
	if dependencies.Store == nil || dependencies.Store.vault == nil || dependencies.Store.workspaceUUID == "" ||
		dependencies.Delivery == nil || dependencies.Mutations == nil || dependencies.InvalidateSessions == nil ||
		dependencies.BindingTargets == nil || dependencies.AllowGenerate == nil || dependencies.AllowReveal == nil {
		return nil, ErrRuntimeUnavailable
	}
	now := dependencies.Now
	if now == nil {
		now = time.Now
	}
	nonce := dependencies.Nonce
	if nonce == nil {
		nonce = newPreviewNonce
	}
	return &Runtime{
		store: dependencies.Store, delivery: dependencies.Delivery, mutations: dependencies.Mutations,
		invalidateSessions: dependencies.InvalidateSessions,
		bindingTargets:     dependencies.BindingTargets,
		allowGenerate:      dependencies.AllowGenerate, allowReveal: dependencies.AllowReveal,
		now: now, nonce: nonce, previewNonces: make(map[int64]string),
	}, nil
}

func newPreviewNonce() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate Vault preview nonce: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func (r *Runtime) List(ctx context.Context, filter ListFilter) ([]Item, int, error) {
	if r == nil || r.store == nil {
		return nil, 0, ErrRuntimeUnavailable
	}
	return r.store.List(ctx, filter)
}

func (r *Runtime) Get(ctx context.Context, id int64) (Item, error) {
	if r == nil || r.store == nil {
		return Item{}, ErrRuntimeUnavailable
	}
	return r.store.Get(ctx, id)
}

func (r *Runtime) Create(ctx context.Context, input CreateInput) (Item, error) {
	if r == nil || r.store == nil || r.delivery == nil || r.mutations == nil {
		return Item{}, ErrRuntimeUnavailable
	}
	release, err := r.delivery.AcquireExclusive(ctx)
	if err != nil {
		return Item{}, err
	}
	defer release()
	if err := r.requireFinalizationsReady(ctx); err != nil {
		return Item{}, err
	}
	var item Item
	err = r.mutations.WithMutation(ctx, "vault.item.created", func() any {
		return ItemAuditPayload(item)
	}, func(tx *sql.Tx) error {
		var createErr error
		item, createErr = r.store.WithTx(tx).Create(ctx, input)
		return createErr
	})
	return item, err
}

func (r *Runtime) UpdateMetadata(ctx context.Context, input UpdateMetadataInput) (Item, error) {
	if err := r.validateMutation(); err != nil {
		return Item{}, err
	}
	release, err := r.delivery.AcquireExclusive(ctx)
	if err != nil {
		return Item{}, err
	}
	defer release()
	if err := r.requireFinalizationsReady(ctx); err != nil {
		return Item{}, err
	}
	current, err := r.store.Get(ctx, input.ID)
	if err != nil {
		return Item{}, err
	}
	if current.MetadataRevision != input.ExpectedMetadataRevision {
		return Item{}, ErrStale
	}
	scope := SessionMutationScope{ItemID: input.ID}
	sessions, err := r.store.ActiveSessionsForMutation(ctx, scope)
	if err != nil {
		return Item{}, err
	}
	var item Item
	var finalizationID int64
	err = r.mutations.WithMutation(ctx, "vault.item.updated", func() any {
		return ItemAuditPayload(item)
	}, func(tx *sql.Tx) error {
		var updateErr error
		item, updateErr = r.store.WithTx(tx).UpdateMetadata(ctx, input)
		if updateErr != nil {
			return updateErr
		}
		finalizationID, updateErr = r.queueSessionFinalization(ctx, tx, sessions, scope)
		return updateErr
	})
	if err != nil {
		return Item{}, err
	}
	if err := r.finishSessionFinalization(ctx, finalizationID, sessions, scope); err != nil {
		return item, err
	}
	return item, nil
}

func (r *Runtime) Delete(ctx context.Context, id, expectedValueVersion, expectedMetadataRevision int64) error {
	if err := r.validateMutation(); err != nil {
		return err
	}
	release, err := r.delivery.AcquireExclusive(ctx)
	if err != nil {
		return err
	}
	defer release()
	if err := r.requireFinalizationsReady(ctx); err != nil {
		return err
	}
	item, err := r.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if item.ValueVersion != expectedValueVersion || item.MetadataRevision != expectedMetadataRevision {
		return ErrStale
	}
	scope := SessionMutationScope{ItemID: item.ID}
	sessions, err := r.store.ActiveSessionsForMutation(ctx, scope)
	if err != nil {
		return err
	}
	var finalizationID int64
	if err := r.mutations.WithMutation(ctx, "vault.item.deleted", func() any {
		return ItemAuditPayload(item)
	}, func(tx *sql.Tx) error {
		if err := r.store.WithTx(tx).Delete(ctx, id, expectedValueVersion, expectedMetadataRevision); err != nil {
			return err
		}
		var err error
		finalizationID, err = r.queueSessionFinalization(ctx, tx, sessions, scope)
		return err
	}); err != nil {
		return err
	}
	if err := r.finishSessionFinalization(ctx, finalizationID, sessions, scope); err != nil {
		return err
	}
	r.clearPreview(id)
	return nil
}

func (r *Runtime) validateMutation() error {
	if r == nil || r.store == nil || r.delivery == nil || r.mutations == nil || r.invalidateSessions == nil {
		return ErrRuntimeUnavailable
	}
	return nil
}

func ItemAuditPayload(item Item) map[string]any {
	return map[string]any{
		"project_id": item.OwnerProjectID, "vault_item_id": item.ID, "name": item.Name,
		"value_version": item.ValueVersion, "metadata_revision": item.MetadataRevision,
	}
}
