package gatewayoperations

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/messagequeue"
)

type MessageRedactor func(context.Context, string) string

type MessageStore struct {
	store *messagequeue.Store
}

func NewMessageStore(database *sql.DB, redact MessageRedactor) *MessageStore {
	if database == nil || redact == nil {
		return nil
	}
	return &MessageStore{store: messagequeue.NewStore(database, messagequeue.Redactor(redact))}
}

type MessageScope struct {
	Store *MessageStore
}

type MessageScopeProvider func(http.ResponseWriter) (MessageScope, bool)

type MessageHTTPHandlers struct {
	scope MessageScopeProvider
}

func NewMessageHTTPHandlers(scope MessageScopeProvider) *MessageHTTPHandlers {
	return &MessageHTTPHandlers{scope: scope}
}

func (handlers *MessageHTTPHandlers) List(w http.ResponseWriter, request *http.Request) {
	store, ok := handlers.resolve(w)
	if !ok {
		return
	}
	filter := messagequeue.Filter{Direction: strings.TrimSpace(request.URL.Query().Get("direction"))}
	if raw := strings.TrimSpace(request.URL.Query().Get("runtime_id")); raw != "" {
		runtimeID, valid := httptransport.ParseQueryInt64(w, raw, "runtime_id")
		if !valid {
			return
		}
		filter.RuntimeID = runtimeID
	}
	items, err := store.store.List(request.Context(), filter)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, items)
}

func (handlers *MessageHTTPHandlers) Create(w http.ResponseWriter, request *http.Request) {
	store, ok := handlers.resolve(w)
	if !ok {
		return
	}
	var input messagequeue.CreateRequest
	if !httptransport.DecodeJSON(w, request, &input, httptransport.DefaultJSONBodyBytes) {
		return
	}
	item, err := store.store.Insert(request.Context(), input)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	httptransport.WriteJSON(w, http.StatusCreated, item)
}

func (handlers *MessageHTTPHandlers) MarkRead(w http.ResponseWriter, request *http.Request) {
	store, ok := handlers.resolve(w)
	if !ok {
		return
	}
	var input struct {
		RuntimeID int64 `json:"runtime_id"`
	}
	if !httptransport.DecodeJSON(w, request, &input, httptransport.DefaultJSONBodyBytes) {
		return
	}
	if input.RuntimeID < 1 {
		httptransport.WriteError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	count, err := store.store.MarkRuntimeRead(request.Context(), input.RuntimeID)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"status": "read", "count": count})
}

func (handlers *MessageHTTPHandlers) resolve(w http.ResponseWriter) (*MessageStore, bool) {
	if handlers == nil || handlers.scope == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	store, ok := handlers.scope(w)
	if !ok {
		return nil, false
	}
	if store.Store == nil || store.Store.store == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	return store.Store, true
}
