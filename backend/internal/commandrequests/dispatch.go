package commandrequests

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/commandrequests/finality"
	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

// ClaimDispatch must commit before remote execution. A failed claim never
// authorizes a send; a committed claim only proves admission, not delivery.
func (s *Store) ClaimDispatch(ctx context.Context, projection Projection, id int64) error {
	return s.withProjectionTransaction(ctx, projection, func(executor Executor) ([]int64, error) {
		result, err := executor.ExecContext(ctx, finality.ClaimSQL, id)
		if err != nil {
			return nil, err
		}
		affected, err := sqldb.RowsAffected(result, "claim command dispatch")
		if err != nil {
			return nil, err
		}
		if affected != 1 {
			return nil, ErrNotRunning
		}
		return []int64{id}, nil
	})
}

func (r *Runtime) ClaimDispatch(ctx context.Context, id int64) error {
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.ClaimDispatch(ctx, r.projection, id)
}
