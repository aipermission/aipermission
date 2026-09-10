package observation

import (
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/retention"
)

type State struct {
	AuditDispatcher *observability.Dispatcher
	Retention       *retention.Service
}
