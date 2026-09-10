package projectvault

import (
	"context"
	"database/sql"
)

type DefaultBindingFilter struct {
	VaultItemID int64
	TargetID    int64
	ProfileID   int64
}

func (r *Runtime) ListDefaultBindings(ctx context.Context, filter DefaultBindingFilter) ([]DefaultBinding, error) {
	if r == nil || r.store == nil {
		return nil, ErrRuntimeUnavailable
	}
	return r.store.ListDefaultBindings(ctx, filter.VaultItemID, filter.TargetID, filter.ProfileID)
}

func (r *Runtime) SaveDefaultBinding(ctx context.Context, input DefaultBindingInput) (DefaultBinding, error) {
	if err := r.validateBindingMutation(); err != nil {
		return DefaultBinding{}, err
	}
	release, err := r.delivery.AcquireExclusive(ctx)
	if err != nil {
		return DefaultBinding{}, err
	}
	defer release()
	if err := r.bindingTargets.ValidateDefaultBindingTarget(ctx, input.TargetID, input.ProfileID); err != nil {
		return DefaultBinding{}, err
	}
	existing, found, err := r.store.FindDefaultBinding(ctx, input)
	if err != nil {
		return DefaultBinding{}, err
	}
	if (found && existing.BindingRevision != input.ExpectedBindingRevision) ||
		(!found && input.ExpectedBindingRevision != 0) {
		return DefaultBinding{}, ErrStale
	}
	if found && existing.ReplaceExisting == input.ReplaceExisting {
		return existing, nil
	}
	sessions := []SessionReference{}
	if found {
		sessions, err = r.store.ActiveSessionsForMutation(ctx, SessionMutationScope{BindingID: existing.ID})
		if err != nil {
			return DefaultBinding{}, err
		}
	}
	var binding DefaultBinding
	err = r.mutations.WithMutation(ctx, "vault.binding.updated", func() any {
		return BindingAuditPayload(binding)
	}, func(tx *sql.Tx) error {
		var saveErr error
		binding, saveErr = r.store.WithTx(tx).SaveDefaultBinding(ctx, input)
		return saveErr
	})
	if err != nil {
		return DefaultBinding{}, err
	}
	if err := r.invalidateSessions(ctx, sessions, SessionMutationScope{BindingID: binding.ID}); err != nil {
		return DefaultBinding{}, err
	}
	return binding, nil
}

func (r *Runtime) DeleteDefaultBinding(ctx context.Context, id, expectedRevision int64) error {
	if err := r.validateBindingMutation(); err != nil {
		return err
	}
	release, err := r.delivery.AcquireExclusive(ctx)
	if err != nil {
		return err
	}
	defer release()
	binding, err := r.store.GetDefaultBinding(ctx, id)
	if err != nil {
		return err
	}
	if binding.BindingRevision != expectedRevision {
		return ErrStale
	}
	scope := SessionMutationScope{BindingID: binding.ID}
	sessions, err := r.store.ActiveSessionsForMutation(ctx, scope)
	if err != nil {
		return err
	}
	if err := r.mutations.WithMutation(ctx, "vault.binding.deleted", func() any {
		return map[string]any{"binding_id": id}
	}, func(tx *sql.Tx) error {
		return r.store.WithTx(tx).DeleteDefaultBinding(ctx, id, expectedRevision)
	}); err != nil {
		return err
	}
	return r.invalidateSessions(ctx, sessions, scope)
}

func (r *Runtime) validateBindingMutation() error {
	if err := r.validateMutation(); err != nil {
		return err
	}
	if r.bindingTargets == nil {
		return ErrRuntimeUnavailable
	}
	return nil
}

func BindingAuditPayload(binding DefaultBinding) map[string]any {
	return map[string]any{
		"binding_id": binding.ID, "vault_item_id": binding.VaultItemID,
		"source_project_id": binding.SourceProjectID,
		"target_id":         binding.TargetID, "profile_id": binding.ProfileID,
		"binding_revision": binding.BindingRevision,
	}
}
