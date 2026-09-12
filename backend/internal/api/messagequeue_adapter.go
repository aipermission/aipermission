package api

import (
	"context"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	"net/http"

	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func (s *Server) messageQueueScope(w http.ResponseWriter) (gatewayoperations.MessageScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return gatewayoperations.MessageScope{}, false
	}
	return gatewayoperations.MessageScope{Store: s.messageQueueStore(runtime)}, true
}

func (s *Server) messageQueueStore(runtime *gatewayinfra.WorkspaceHandle) *gatewayoperations.MessageStore {
	store, _ := s.operationsOwner.MessageStore(runtime, func(ctx context.Context, value string) string {
		return s.redactForPersistence(ctx, runtime, value)
	})
	return store
}
