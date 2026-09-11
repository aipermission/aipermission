package gatewayoperations

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCommandRuntimeFailsClosedUntilInitialized(t *testing.T) {
	component := &CommandComponent{}
	if _, err := component.Runtime("runtime"); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("uninitialized command runtime error = %v", err)
	}
	if err := component.Initialize("runtime", CommandRuntimeDependencies{}); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("invalid command runtime dependencies error = %v", err)
	}
	var unavailable *CommandComponent
	if err := unavailable.Initialize("runtime", CommandRuntimeDependencies{}); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("nil component initialization error = %v", err)
	}
	if err := component.Initialize(" ", CommandRuntimeDependencies{}); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("blank runtime initialization error = %v", err)
	}
}

func TestCommandHTTPHandlersFailClosedWithNilComponent(t *testing.T) {
	var component *CommandComponent
	handlers := component.HTTPHandlers(CommandScopeProviders{})
	if handlers.Bulk != nil || handlers.Requests != nil {
		t.Fatal("nil command component exposed HTTP handlers")
	}
}

func TestCommandHTTPHandlersOwnFailClosedAdapters(t *testing.T) {
	handlers := (&CommandComponent{}).HTTPHandlers(CommandScopeProviders{})
	if handlers.Bulk == nil || handlers.Requests == nil {
		t.Fatal("command component did not expose owned HTTP handlers")
	}
	tests := []struct {
		name string
		run  func(http.ResponseWriter, *http.Request)
	}{
		{name: "bulk", run: handlers.Bulk.Run},
		{name: "request", run: handlers.Requests.Get},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.SetPathValue("id", "1")
			test.run(response, request)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d", response.Code)
			}
		})
	}
}

func TestCommandRuntimeWrapperFailsClosedWithoutOwner(t *testing.T) {
	runtime := &CommandRuntime{}
	if err := runtime.CancelRunning(t.Context(), "closing"); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("CancelRunning() error = %v", err)
	}
	if _, err := runtime.CancelRunningForRuntime(t.Context(), 1, "closing"); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("CancelRunningForRuntime() error = %v", err)
	}
}

func TestCommandBulkHTTPHandlerRejectsIncompleteRuntime(t *testing.T) {
	handlers := (&CommandComponent{}).HTTPHandlers(CommandScopeProviders{
		Bulk: func(http.ResponseWriter) (*CommandBulkHTTPRuntime, bool) {
			return &CommandBulkHTTPRuntime{}, true
		},
	})
	response := httptest.NewRecorder()
	handlers.Bulk.Run(response, httptest.NewRequest(http.MethodPost, "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
}
