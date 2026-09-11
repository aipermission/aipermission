// Package observation defines the workspace boundary's observation-worker port.
package observation

import (
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/retention"
)

type Port interface {
	AuditDispatcherService() *observability.Dispatcher
	SetAuditDispatcherService(*observability.Dispatcher)
	RetentionService() *retention.Service
	SetRetentionService(*retention.Service)
}
