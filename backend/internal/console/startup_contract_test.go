package console

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

func TestConsoleStartupFailuresPreserveNotificationPersistenceAndOwnership(t *testing.T) {
	for _, scenario := range []struct {
		phase   string
		message string
		closed  int
	}{
		{"missing opener", "console transport is not configured", 0},
		{"open failed", "fixture startup failure", 0},
		{"nil runtime", "console transport returned no session", 0},
		{"missing stdin", "console transport did not provide stdin", 1},
		{"environment", "fixture startup failure", 1},
		{"missing output", "console transport did not provide output", 1},
		{"missing completion", "console transport did not provide completion signal", 1},
	} {
		t.Run(scenario.phase, func(t *testing.T) {
			_, manager, session := newManualHistoryTestSession(t)
			session.ctx, session.cancel = context.WithCancel(t.Context())
			t.Cleanup(session.cancel)
			session.start = make(chan struct{})
			session.done = make(chan struct{})
			cause := errors.New("fixture startup failure")
			closed := 0
			if scenario.phase == "environment" {
				var err error
				session.environment, err = sessionenv.NewEnvelope([]sessionenv.EntryInput{{Name: "FIXTURE_VALUE", Value: []byte("private-fixture-value")}})
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario.phase != "missing opener" {
				manager.openRuntime = func(context.Context, RuntimeOpenRequest) (*RuntimeSession, error) {
					if scenario.phase == "open failed" {
						return nil, cause
					}
					if scenario.phase == "nil runtime" {
						return nil, nil
					}
					done := make(chan error)
					close(done)
					runtime := &RuntimeSession{Stdin: &recordingWriteCloser{}, Output: testRuntimeOutput(), Done: done,
						Close:            func() error { closed++; return nil },
						ApplyEnvironment: func(context.Context, *sessionenv.Envelope) error { return cause },
					}
					switch scenario.phase {
					case "missing stdin":
						runtime.Stdin = nil
					case "missing output":
						runtime.Output = nil
					case "missing completion":
						runtime.Done = nil
					}
					return runtime, nil
				}
			}
			session.run()
			select {
			case <-session.done:
			default:
				t.Fatal("startup failure did not join the session lifecycle")
			}
			if err := session.waitStart(t.Context()); err == nil || !strings.Contains(err.Error(), scenario.message) {
				t.Fatalf("startup notification=%v, want %q", err, scenario.message)
			} else if (scenario.phase == "open failed" || scenario.phase == "environment") && !errors.Is(err, cause) {
				t.Fatal("startup notification lost underlying error classification")
			}
			record, err := manager.Get(t.Context(), session.id)
			if err != nil || record.Status != "error" || !strings.Contains(record.Error, scenario.message) || closed != scenario.closed {
				t.Fatalf("failure record=%#v closed=%d, err=%v", record, closed, err)
			}
		})
	}
}
