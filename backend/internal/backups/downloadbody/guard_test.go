package downloadbody

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGuardBoundsHeadersIdleBodyAndTotalTime(t *testing.T) {
	for _, test := range []struct {
		name        string
		total, idle time.Duration
		want        error
	}{
		{"idle before headers", time.Second, 10 * time.Millisecond, context.Canceled},
		{"total transfer", 10 * time.Millisecond, time.Second, context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			guard := New(t.Context(), test.total, test.idle)
			defer guard.Close()
			select {
			case <-guard.Done():
			case <-time.After(time.Second):
				t.Fatal("transfer was not bounded")
			}
			if n, err := guard.Reader(strings.NewReader("data")).Read(make([]byte, 8)); n != 0 || !errors.Is(err, test.want) {
				t.Fatalf("expired read: %d %v", n, err)
			}
		})
	}
}

func TestGuardProgressRenewsOnlyAnUnexpiredDeadline(t *testing.T) {
	guard := New(t.Context(), time.Second, time.Hour)
	defer guard.Close()
	initial := guard.deadline
	if n, err := guard.Reader(strings.NewReader("data")).Read(make([]byte, 4)); n != 4 || err != nil {
		t.Fatalf("progress read: %d %v", n, err)
	}
	guard.mu.Lock()
	updated := guard.deadline
	guard.mu.Unlock()
	if !updated.After(initial) {
		t.Fatal("body progress did not renew idle deadline")
	}
	guard.expire()
	if guard.Err() != nil {
		t.Fatal("stale timer callback expired a renewed deadline")
	}
	guard.mu.Lock()
	guard.deadline = time.Now().Add(-time.Second)
	guard.mu.Unlock()
	if _, err := guard.Reader(strings.NewReader("late data")).Read(make([]byte, 16)); !errors.Is(err, context.Canceled) {
		t.Fatalf("late progress revived an expired transfer: %v", err)
	}
	guard.expire()
}

func TestGuardEOFFinishesIdleWatchAndCloseIsIdempotent(t *testing.T) {
	guard := New(t.Context(), time.Second, time.Hour)
	if _, err := guard.Reader(strings.NewReader("")).Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("EOF = %v", err)
	}
	guard.mu.Lock()
	guard.deadline = time.Now().Add(-time.Second)
	guard.mu.Unlock()
	guard.expire()
	if guard.Err() != nil {
		t.Fatal("EOF left the no-progress watchdog active")
	}
	guard.Close()
	guard.Close()
	if !errors.Is(guard.Err(), context.Canceled) {
		t.Fatal("close did not cancel transfer context")
	}
}

func TestGuardParentCancellationAndEmptyReadsDoNotExtendDeadline(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	guard := New(parent, time.Second, time.Hour)
	defer guard.Close()
	deadline := guard.deadline
	if n, err := guard.Reader(emptyReader{}).Read(make([]byte, 1)); n != 0 || err != nil {
		t.Fatalf("empty read = %d %v", n, err)
	}
	if !guard.deadline.Equal(deadline) {
		t.Fatal("empty read renewed no-progress deadline")
	}
	cancel()
	if _, err := guard.Reader(strings.NewReader("ignored")).Read(make([]byte, 8)); !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation ignored: %v", err)
	}
}

type emptyReader struct{}

func (emptyReader) Read([]byte) (int, error) { return 0, nil }

func TestGuardCancelsActualHTTPHeaderAndBodyReads(t *testing.T) {
	for _, flushHeaders := range []bool{false, true} {
		t.Run(map[bool]string{false: "headers", true: "body"}[flushHeaders], func(t *testing.T) {
			stopped := make(chan struct{})
			service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if flushHeaders {
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
				close(stopped)
			}))
			defer service.Close()
			guard := New(t.Context(), time.Second, 30*time.Millisecond)
			defer guard.Close()
			request, err := http.NewRequestWithContext(guard, http.MethodGet, service.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := service.Client().Do(request)
			if flushHeaders {
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				_, err = io.ReadAll(guard.Reader(response.Body))
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("unresponsive transfer: %v", err)
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("idle cancel did not release the transport")
			}
		})
	}
}
