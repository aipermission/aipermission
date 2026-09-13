package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdapterRoutesAreRegisteredByHTTPTransport(t *testing.T) {
	mux := http.NewServeMux()
	registerAdapterRoutes(mux, []AdapterRoute{{
		Kind:   "test",
		Method: http.MethodGet,
		Path:   "/api/connectors/test/owned",
		Policy: AdapterRoutePolicyUIRead,
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	}})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/connectors/test/owned", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("adapter route status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestAdapterRoutesFailClosedBeforeMuxRegistration(t *testing.T) {
	for _, route := range []AdapterRoute{
		{Kind: "test", Method: http.MethodGet, Path: "/api/connectors/test/credentials", Policy: AdapterRoutePolicyUIRead, Handler: func(http.ResponseWriter, *http.Request) {}},
		{Kind: "other", Method: http.MethodGet, Path: "/api/connectors/test/owned", Policy: AdapterRoutePolicyUIRead, Handler: func(http.ResponseWriter, *http.Request) {}},
		{Method: http.MethodGet, Path: "/outside-api", Policy: AdapterRoutePolicyUIRead, Handler: func(http.ResponseWriter, *http.Request) {}},
		{Method: http.MethodPost, Path: "/api/mcp/connector-owned", Policy: AdapterRoutePolicyUIMutation, Handler: func(http.ResponseWriter, *http.Request) {}},
		{Method: http.MethodGet, Path: "/api/missing-handler", Policy: AdapterRoutePolicyUIRead},
		{Method: http.MethodGet, Path: "/api/missing-policy", Handler: func(http.ResponseWriter, *http.Request) {}},
		{Method: http.MethodPost, Path: "/api/read-mutation", Policy: AdapterRoutePolicyUIRead, Handler: func(http.ResponseWriter, *http.Request) {}},
		{Method: http.MethodGet, Path: "/api/mutation-read", Policy: AdapterRoutePolicyUIMutation, Handler: func(http.ResponseWriter, *http.Request) {}},
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
