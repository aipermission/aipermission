package providerlock

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/backups/operationcoord"
)

type key struct {
	database   *sql.DB
	providerID int64
}

// Manager serializes one provider without coupling unrelated workspaces or providers.
type Manager struct {
	coordinator operationcoord.Coordinator[key]
}

func (m *Manager) Acquire(ctx context.Context, database *sql.DB, providerID int64) (func(), error) {
	return m.coordinator.Acquire(ctx, key{database: database, providerID: providerID})
}
