package projectvault

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/vaultfinalization"
)

func (r *Runtime) queueSessionFinalization(ctx context.Context, tx *sql.Tx, sessions []SessionReference, scope SessionMutationScope) (int64, error) {
	references := make([]vaultfinalization.Reference, len(sessions))
	for index, session := range sessions {
		references[index] = vaultfinalization.Reference{
			SessionID: session.SessionID, RuntimeID: session.RuntimeID, Generation: session.Generation,
		}
	}
	return vaultfinalization.NewStore(tx).Queue(ctx, vaultfinalization.Intent{
		Kind: "mutation", ItemID: scope.ItemID, BindingID: scope.BindingID, References: references,
	})
}

func (r *Runtime) finishSessionFinalization(ctx context.Context, id int64, sessions []SessionReference, scope SessionMutationScope) error {
	return vaultfinalization.NewStore(r.store.db).Finalize(ctx, id, func(cleanupCtx context.Context) error {
		return r.invalidateSessions(cleanupCtx, sessions, scope)
	})
}

func (r *Runtime) requireFinalizationsReady(ctx context.Context) error {
	return vaultfinalization.NewStore(r.store.db).RequireReady(ctx)
}
