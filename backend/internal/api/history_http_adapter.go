package api

import (
	"context"
	"database/sql"
	"net/http"

	historyhttp "github.com/aipermission/aipermission/backend/internal/history"
)

func (s *Server) historyHTTPScope(w http.ResponseWriter) (historyhttp.Scope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return historyhttp.Scope{}, false
	}
	return historyhttp.Scope{
		Database: runtime.Storage.Database,
		Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
	}, true
}
