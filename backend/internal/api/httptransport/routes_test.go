package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdapterRoutesAreRegisteredByHTTPTransport(t *testing.T) {
	mux := http.NewServeMux()
	registerAdapterRoutes(mux, []AdapterRoute{{
		Method: http.MethodGet,
		Path:   "/api/connector-owned",
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	}})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/connector-owned", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("adapter route status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestAdapterRoutesFailClosedBeforeMuxRegistration(t *testing.T) {
	for _, route := range []AdapterRoute{
		{Method: http.MethodGet, Path: "/outside-api", Handler: func(http.ResponseWriter, *http.Request) {}},
		{Method: http.MethodGet, Path: "/api/missing-handler"},
	} {
		t.Run(route.Path, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid adapter route did not fail closed")
				}
			}()
			registerAdapterRoutes(http.NewServeMux(), []AdapterRoute{route})
		})
	}
}
