package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
	handler := withRequestDeadline(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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
			handler := withRequestDeadline(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				if _, ok := r.Context().Deadline(); ok {
					t.Fatalf("streaming route %s received ordinary deadline", path)
				}
			}), time.Second)
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		})
	}
}

func TestUnboundedTransfersDoNotBypassLifecycleLocks(t *testing.T) {
	if !isStreamingRoute("/api/console/sessions/12/attach") {
		t.Fatal("console attach must retain its streaming lifecycle behavior")
	}
	if !isStreamingRoute("/api/settings/maintenance-console/attach") {
		t.Fatal("maintenance console attach must not retain a lifecycle lock for the WebSocket lifetime")
	}
	for _, path := range []string{
		"/api/backup/download",
		"/api/backup/import",
		"/api/file-transfers/upload",
		"/api/connector-targets/1/profiles/2/restore",
	} {
		if isStreamingRoute(path) {
			t.Fatalf("unbounded transfer route %s must not bypass lifecycle locking", path)
		}
		if !isUnboundedRequestRoute(path) {
			t.Fatalf("transfer route %s must remain exempt from ordinary deadlines", path)
		}
	}
}

func TestRemoteBrowseKeepsItsLongerBoundedDeadline(t *testing.T) {
	if got := requestTimeoutForPath("/api/file-transfers/expand", ordinaryRequestTimeout); got != remoteBrowseRequestTimeout {
		t.Fatalf("remote expand timeout = %s, want %s", got, remoteBrowseRequestTimeout)
	}
	if got := requestTimeoutForPath("/api/status", ordinaryRequestTimeout); got != ordinaryRequestTimeout {
		t.Fatalf("ordinary timeout = %s, want %s", got, ordinaryRequestTimeout)
	}
}

func TestOrdinaryRouteAppliesTransportDeadlines(t *testing.T) {
	writer := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	deadlineSeen := make(chan [2]time.Time, 1)
	handler := withRequestDeadline(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
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
