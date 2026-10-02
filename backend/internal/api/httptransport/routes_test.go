package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type transferControlRouteProbe struct {
	FileTransfers
	action string
	id     string
}

func (probe *transferControlRouteProbe) record(w http.ResponseWriter, r *http.Request, action string) {
	probe.action, probe.id = action, r.PathValue("id")
	w.WriteHeader(http.StatusNoContent)
}

func (probe *transferControlRouteProbe) CancelFileTransfer(w http.ResponseWriter, r *http.Request) {
	probe.record(w, r, "cancel file")
}

func (probe *transferControlRouteProbe) CancelFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	probe.record(w, r, "cancel batch")
}

func (probe *transferControlRouteProbe) PauseFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	probe.record(w, r, "pause batch")
}

func (probe *transferControlRouteProbe) ResumeFileTransferBatch(w http.ResponseWriter, r *http.Request) {
	probe.record(w, r, "resume batch")
}

func TestFileTransferControlRoutesDispatchToMatchingOwnerMethods(t *testing.T) {
	for _, testCase := range []struct{ path, action string }{
		{"/api/file-transfers/37/cancel", "cancel file"},
		{"/api/file-transfer-batches/37/cancel", "cancel batch"},
		{"/api/file-transfer-batches/37/pause", "pause batch"},
		{"/api/file-transfer-batches/37/resume", "resume batch"},
	} {
		t.Run(testCase.action, func(t *testing.T) {
			probe := &transferControlRouteProbe{}
			mux := http.NewServeMux()
			registerTransfers(mux, probe)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, testCase.path, nil))
			if response.Code != http.StatusNoContent || probe.action != testCase.action || probe.id != "37" {
				t.Fatalf("control route response=%d action=%q id=%q", response.Code, probe.action, probe.id)
			}
			response = httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, testCase.path, nil))
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("GET control response=%d, want 405", response.Code)
			}
		})
	}
}

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
