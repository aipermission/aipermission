package observation

import (
	"context"
	"database/sql"

	connectorcatalog "github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/retention"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
)

type Capability struct {
	Database            *sql.DB
	Registry            *connectorcatalog.Registry
	MCPStarted          func() bool
	PrepareRedactor     func(context.Context) func(string) string
	AuditDispatcher     func() *observability.Dispatcher
	SetAuditDispatcher  func(*observability.Dispatcher)
	RetentionService    func() *retention.Service
	SetRetentionService func(*retention.Service)
}

func New(database *sql.DB, registry *connectorcatalog.Registry, mcpStarted func() bool, policy *securitypolicy.Service, auditDispatcher func() *observability.Dispatcher, setAuditDispatcher func(*observability.Dispatcher), retentionService func() *retention.Service, setRetentionService func(*retention.Service)) Capability {
	prepareRedactor := func(ctx context.Context) func(string) string {
		if policy == nil {
			return nil
		}
		return policy.PrepareRedactor(ctx)
	}
	return Capability{
		Database: database, Registry: registry, MCPStarted: mcpStarted, PrepareRedactor: prepareRedactor,
		AuditDispatcher: auditDispatcher, SetAuditDispatcher: setAuditDispatcher,
		RetentionService: retentionService, SetRetentionService: setRetentionService,
	}
}
