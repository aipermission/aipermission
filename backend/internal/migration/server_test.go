package migration

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandlerAddsBoundedMigrationRequestDeadline(t *testing.T) {
	server := &Server{mux: http.NewServeMux()}
	server.mux.HandleFunc("GET /deadline", func(_ http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Fatal("migration request has no deadline")
		}
		if remaining := time.Until(deadline); remaining <= 0 || remaining > migrationRequestTimeout {
			t.Fatalf("migration deadline = %s, want at most %s", remaining, migrationRequestTimeout)
		}
	})
	server.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/deadline", nil))
}
