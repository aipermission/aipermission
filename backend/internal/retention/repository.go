package retention

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

type repository interface {
	ReadSettings(context.Context, *sql.DB, []string) (map[string]string, error)
	WriteSetting(context.Context, sqldb.Executor, string, string, string) error
	PurgeHistory(context.Context, sqldb.Executor, string) (int64, error)
	PurgeAudit(context.Context, sqldb.Executor, string) (int64, error)
	PurgeConsole(context.Context, sqldb.Executor, string) (int64, error)
	PurgeMessages(context.Context, sqldb.Executor, string) (int64, error)
	PurgeExpiredIdempotency(context.Context, sqldb.Executor) (int64, error)
}
