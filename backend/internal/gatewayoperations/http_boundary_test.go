package gatewayoperations

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortransport"
)

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	readDeadline  time.Time
	writeDeadline time.Time
}

func (w *deadlineRecorder) SetReadDeadline(deadline time.Time) error {
	w.readDeadline = deadline
	return nil
}

func (w *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	w.writeDeadline = deadline
	return nil
}

func TestRequestDeadlineBoundsOrdinaryRoutes(t *testing.T) {
	deadlineObserved := make(chan bool, 1)
	handler := WithRequestDeadline(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, ok := r.Context().Deadline()
		deadlineObserved <- ok
		<-r.Context().Done()
	}), 10*time.Millisecond)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if !<-deadlineObserved {
		t.Fatal("ordinary route did not receive a request deadline")
	}
}

func TestRequestDeadlineLeavesStreamingRoutesUnbounded(t *testing.T) {
	paths := []string{
		"/api/settings/maintenance-console/attach",
		"/api/console/sessions/12/attach",
		"/api/backup/download",
		"/api/backup/import",
		"/api/backup/remote/restore",
		"/api/backup/providers/3/upload",
		"/api/backup/providers/3/records/9/download",
		"/api/backup/providers/3/records/9/restore",
		"/api/file-transfers/4/download",
		"/api/file-transfer-batches/4/download",
		"/api/file-transfers/upload",
		"/api/file-transfers/upload-batch",
		"/api/connector-targets/1/profiles/2/backup",
		"/api/connector-targets/1/profiles/2/restore",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			handler := WithRequestDeadline(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				if _, ok := r.Context().Deadline(); ok {
					t.Fatalf("streaming route %s received ordinary deadline", path)
				}
			}), time.Second)
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		})
	}
}

func TestUnboundedTransfersDoNotBypassLifecycleLocks(t *testing.T) {
	if !IsStreamingRoute("/api/console/sessions/12/attach") {
		t.Fatal("console attach must retain its streaming lifecycle behavior")
	}
	if !IsStreamingRoute("/api/settings/maintenance-console/attach") {
		t.Fatal("maintenance console attach must not retain a lifecycle lock for the WebSocket lifetime")
	}
	for _, path := range []string{
		"/api/backup/download",
		"/api/backup/import",
		"/api/file-transfers/upload",
		"/api/connector-targets/1/profiles/2/restore",
	} {
		if IsStreamingRoute(path) {
			t.Fatalf("unbounded transfer route %s must not bypass lifecycle locking", path)
		}
		if !IsUnboundedRequestRoute(path) {
			t.Fatalf("transfer route %s must remain exempt from ordinary deadlines", path)
		}
	}
}

func TestProviderRecordRestoreIsLifecycleMutation(t *testing.T) {
	if !IsLifecycleMutation("/api/backup/providers/3/records/9/restore") {
		t.Fatal("provider record restore must hold the exclusive workspace lifecycle lock")
	}
	for _, path := range []string{
		"/api/backup/providers/3/records/9/download",
		"/api/backup/providers/3/records",
		"/api/backup/providers/3/restore-preview",
	} {
		if IsLifecycleMutation(path) {
			t.Fatalf("non-restore provider route %s was classified as a lifecycle mutation", path)
		}
	}
}

func TestRemoteBrowseKeepsItsLongerBoundedDeadline(t *testing.T) {
	if got := RequestTimeoutForPath("/api/file-transfers/expand", OrdinaryRequestTimeout); got != RemoteBrowseRequestTimeout {
		t.Fatalf("remote expand timeout = %s, want %s", got, RemoteBrowseRequestTimeout)
	}
	if got := RequestTimeoutForPath("/api/status", OrdinaryRequestTimeout); got != OrdinaryRequestTimeout {
		t.Fatalf("ordinary timeout = %s, want %s", got, OrdinaryRequestTimeout)
	}
}

func TestConnectorActionsOutliveTheirInternalExecutionTimeout(t *testing.T) {
	for _, path := range []string{"/api/connector-actions/local-run", "/api/mcp/connector-actions/call"} {
		if got := RequestTimeoutForPath(path, OrdinaryRequestTimeout); got != ConnectorActionRequestTimeout {
			t.Fatalf("connector action timeout for %s = %s, want %s", path, got, ConnectorActionRequestTimeout)
		}
	}
	if ConnectorActionRequestTimeout <= connectortransport.MaxCommandTimeout {
		t.Fatalf("connector action timeout %s must exceed command timeout %s", ConnectorActionRequestTimeout, connectortransport.MaxCommandTimeout)
	}
}

func TestOrdinaryRouteAppliesTransportDeadlines(t *testing.T) {
	writer := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	deadlineSeen := make(chan [2]time.Time, 1)
	handler := WithRequestDeadline(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		deadlineSeen <- [2]time.Time{writer.readDeadline, writer.writeDeadline}
	}), time.Second)
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	deadlines := <-deadlineSeen
	if deadlines[0].IsZero() || deadlines[1].IsZero() {
		t.Fatalf("transport deadlines were not applied: read=%v write=%v", deadlines[0], deadlines[1])
	}
	if !writer.readDeadline.IsZero() || !writer.writeDeadline.IsZero() {
		t.Fatalf("transport deadlines were not cleared for keep-alive reuse: read=%v write=%v", writer.readDeadline, writer.writeDeadline)
	}
}
