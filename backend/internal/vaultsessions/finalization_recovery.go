package vaultsessions

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/vaultfinalization"
)

func (i *Invalidator) RecoverPendingFinalizations(ctx context.Context) error {
	if err := i.validate(); err != nil {
		return err
	}
	store := vaultfinalization.NewStore(i.persistence.database)
	intents, err := store.Pending(ctx)
	if err != nil {
		return err
	}
	for _, intent := range intents {
		intent := intent
		if err := store.Finalize(ctx, intent.ID, func(cleanupCtx context.Context) error {
			references := make([]Reference, len(intent.References))
			for index, reference := range intent.References {
				references[index] = Reference{
					SessionID: reference.SessionID, RuntimeID: reference.RuntimeID, Generation: reference.Generation,
				}
			}
			switch intent.Kind {
			case "project":
				return i.InvalidateProjectReferences(cleanupCtx, references, intent.ProjectID, intent.Reason)
			case "mutation":
				return i.InvalidateMutation(cleanupCtx, references, intent.ItemID, intent.BindingID,
					"Vault item or binding changed; send a fresh request")
			default:
				return ErrInvalidatorUnavailable
			}
		}); err != nil {
			return err
		}
	}
	return nil
}
