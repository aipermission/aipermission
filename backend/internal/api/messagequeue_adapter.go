package api

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/messagequeue"
)

func (s *Server) messageQueueScope(w http.ResponseWriter) (messagequeue.Scope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return messagequeue.Scope{}, false
	}
	return messagequeue.Scope{Store: s.messageQueueStore(runtime)}, true
}

func (s *Server) messageQueueStore(runtime *databaseRuntime) *messagequeue.Store {
	return messagequeue.NewStore(runtime.Storage.Database, func(ctx context.Context, value string) string {
		return s.redactForPersistence(ctx, runtime, value)
	})
}
