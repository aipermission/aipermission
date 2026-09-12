package httptransport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortransport"
)

type canceledLifecycle struct{}

func (canceledLifecycle) AcquireMutationContext(ctx context.Context) (func(), error) {
	return nil, ctx.Err()
}

type failedLifecycle struct{ err error }

func (l failedLifecycle) AcquireMutationContext(context.Context) (func(), error) { return nil, l.err }
func (l failedLifecycle) AcquireReadContext(context.Context) (func(), error)     { return nil, l.err }

func TestBoundaryDistinguishesLifecycleFailureFromRequestExpiry(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	response := httptest.NewRecorder()
	HTTPBoundary{
		Routes:            http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("failed lifecycle reached routes") }),
		Lifecycle:         failedLifecycle{err: errors.New("not initialized")},
		IsLocalRemoteAddr: func(string) bool { return true }, IsLocalhostHeader: func(string) bool { return true },
	}.serveHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func (canceledLifecycle) AcquireReadContext(ctx context.Context) (func(), error) {
	return nil, ctx.Err()
}

func TestBoundaryRejectsRequestWhoseWorkspaceLeaseExpired(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	HTTPBoundary{
		Routes:    http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("expired request reached routes") }),
		Lifecycle: canceledLifecycle{}, IsLocalRemoteAddr: func(string) bool { return true },
		IsLocalhostHeader: func(string) bool { return true }, IsUnlocked: func() bool { return false },
	}.serveHTTP(response, request)
	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestTimeout)
	}
}

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

func TestBackupOperationRoutesManageLifecycleAfterOperationAdmission(t *testing.T) {
	for _, path := range []string{
		"/api/backup/download",
		"/api/backup/providers/3/upload",
		"/api/backup/providers/3/records/9/download",
		"/api/backup/providers/3/records/9/restore",
	} {
		if !ManagesLifecycleLock(path) {
			t.Errorf("backup operation route %s must own operation-before-lifecycle ordering", path)
		}
	}
	for _, path := range []string{
		"/api/backup/providers",
		"/api/backup/providers/3",
		"/api/backup/providers/3/records",
		"/api/backup/providers/3/prune",
	} {
		if ManagesLifecycleLock(path) {
			t.Errorf("ordinary provider route %s unexpectedly manages its lifecycle lock", path)
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
