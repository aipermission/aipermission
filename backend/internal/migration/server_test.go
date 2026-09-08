package migration

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandlerAddsBoundedMigrationRequestDeadline(t *testing.T) {
	server := newTestServer(t)
	server.mux = http.NewServeMux()
	server.mux.HandleFunc("GET /deadline", func(_ http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Fatal("migration request has no deadline")
		}
		if remaining := time.Until(deadline); remaining <= 0 || remaining > RequestTimeout {
			t.Fatalf("migration deadline = %s, want at most %s", remaining, RequestTimeout)
		}
	})
	request := httptest.NewRequest(http.MethodGet, "/deadline", nil)
	request.Host = "localhost:3211"
	request.RemoteAddr = "127.0.0.1:12345"
	server.Handler().ServeHTTP(httptest.NewRecorder(), request)
}
