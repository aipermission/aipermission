// Package scopes owns the project grants initialized during token admission.
package scopes

import (
	"context"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

// InitializeForToken applies the new-token project policy for creation and
// migration. The caller owns the surrounding token-write transaction.
func InitializeForToken(ctx context.Context, executor sqldb.Executor, tokenID int64, createdAt, updatedAt string) error {
	if tokenID < 1 {
		return fmt.Errorf("token_id must be positive")
	}
	if _, err := executor.ExecContext(ctx, `
		INSERT INTO token_project_scopes (token_id, project_id, enabled, created_at, updated_at)
		SELECT ?, id, 1, ?, ? FROM projects WHERE status = 'active'`, tokenID, createdAt, updatedAt); err != nil {
		return fmt.Errorf("initialize token project scopes: %w", err)
	}
	return nil
}
