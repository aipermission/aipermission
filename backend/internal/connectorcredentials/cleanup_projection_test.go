package connectorcredentials

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestCleanupProjectionRejectsUnavailableContextWithoutDispatch(t *testing.T) {
	for _, mode := range []string{"nil context", "canceled", "missing ports"} {
		t.Run(mode, func(t *testing.T) {
			f := newCleanupFixture(t)
			var ctx context.Context = t.Context()
			message := "credential cleanup projection is unavailable"
			switch mode {
			case "nil context":
				ctx = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				message = context.Canceled.Error()
			case "missing ports":
				f.ports.RedactResult = nil
			}
			got, err := f.ports.ProjectCompletedCleanup(ctx, connectors.ActionResult{Status: connectors.ResultCompleted}, nil, actionresult.NewCredentialBoundary(nil))
			if err == nil || err.Error() != message || !reflect.DeepEqual(got, connectors.ActionResult{}) || f.calls != [4]int{} {
				t.Fatalf("unavailable cleanup projected: %#v %v calls=%v", got, err, f.calls)
			}
		})
	}
}

func TestCleanupProjectionRejectsAmbiguousResultsBeforeAndAfterProjection(t *testing.T) {
	for name, ambiguous := range map[string]connectors.ActionResult{
		"empty status": {},
		"running":      {Status: connectors.ResultRunning},
		"failed":       {Status: connectors.ResultFailed},
		"unknown":      {Status: connectors.ResultOutcomeUnknown},
		"error":        {Status: connectors.ResultCompleted, Error: "unconfirmed-secret"},
		"request":      {Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{RequestID: 4}},
		"session":      {Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{SessionID: 4}},
		"generation":   {Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{SessionGeneration: 4}},
		"batch":        {Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{BatchID: 4}},
		"followup":     {Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{FollowupTool: "poll"}},
	} {
		for _, afterProjection := range []bool{false, true} {
			t.Run(name+"/after="+map[bool]string{false: "false", true: "true"}[afterProjection], func(t *testing.T) {
				f := newCleanupFixture(t)
				f.project = func(connectors.ActionResult, actionresult.CredentialBoundary) (connectors.ActionResult, error) {
					return ambiguous, nil
				}
				input, want := ambiguous, [4]int{0, 0, 0, 1}
				if afterProjection {
					input = connectors.ActionResult{Status: connectors.ResultCompleted}
					want[2] = 1
				}
				got, err := f.ports.ProjectCompletedCleanup(t.Context(), input, nil, actionresult.NewCredentialBoundary(nil))
				if err == nil || !reflect.DeepEqual(got, connectors.ActionResult{}) || strings.Contains(err.Error(), "unconfirmed-secret") || f.calls != want {
					t.Fatalf("ambiguous cleanup accepted/leaked: %#v %v calls=%v want=%v", got, err, f.calls, want)
				}
			})
		}
	}
}

func TestCleanupProjectionRedactsFailureBeforeAndAfterOptionalPolicy(t *testing.T) {
	failure := errors.New("known-secret")
	if err := RequireCompletedCleanup(connectors.ActionResult{Status: connectors.ResultRunning}, failure); !errors.Is(err, failure) {
		t.Fatal("cleanup protocol error was overwritten by status")
	}
	for _, mode := range []string{"cleanup error", "projection error", "projected status"} {
		t.Run(mode, func(t *testing.T) {
			f := newCleanupFixture(t)
			boundary := actionresult.NewCredentialBoundary(map[string]any{"password": "known-secret"})
			f.ports.RedactText = func(_ context.Context, text string) string {
				f.calls[3]++
				if strings.Contains(text, "known-secret") {
					t.Fatal("optional policy received an unmasked credential")
				}
				return text + " known-secret"
			}
			var cleanupErr error
			want := [4]int{0, 0, 1, 1}
			if mode == "cleanup error" {
				cleanupErr, want[2] = failure, 0
			} else {
				f.project = func(result connectors.ActionResult, _ actionresult.CredentialBoundary) (connectors.ActionResult, error) {
					if mode == "projection error" {
						return result, failure
					}
					result.Status = "known-secret"
					return result, nil
				}
			}
			got, err := f.ports.ProjectCompletedCleanup(t.Context(), connectors.ActionResult{Status: connectors.ResultCompleted}, cleanupErr, boundary)
			if err == nil || strings.Contains(err.Error(), "known-secret") || !strings.Contains(err.Error(), actionresult.CredentialRedactionMarker) || !reflect.DeepEqual(got, connectors.ActionResult{}) || f.calls != want {
				t.Fatalf("cleanup failure leaked/accepted: %#v %v calls=%v want=%v", got, err, f.calls, want)
			}
			if mode == "projection error" && !strings.HasPrefix(err.Error(), "process credential cleanup result: ") {
				t.Fatal("projection diagnostic lost its context")
			}
		})
	}
}

func TestCleanupProjectionReturnsOnlyProjectedTerminalResult(t *testing.T) {
	f := newCleanupFixture(t)
	boundary := actionresult.NewCredentialBoundary(map[string]any{"password": "known-secret"})
	f.project = func(result connectors.ActionResult, received actionresult.CredentialBoundary) (connectors.ActionResult, error) {
		if result.Output != "known-secret" || received != boundary {
			t.Fatal("projection lost result or credential boundary")
		}
		result.Output = received.Redact(result.Output.(string))
		result.DisplayText = "confirmed"
		return result, nil
	}
	input := connectors.ActionResult{Status: connectors.ResultCompleted, Output: "known-secret"}
	got, err := f.ports.ProjectCompletedCleanup(t.Context(), input, nil, boundary)
	want := connectors.ActionResult{Status: connectors.ResultCompleted, Output: actionresult.CredentialRedactionMarker, DisplayText: "confirmed"}
	if err != nil || !reflect.DeepEqual(got, want) || input.Output != "known-secret" || f.calls != [4]int{0, 0, 1, 0} {
		t.Fatalf("terminal projection=%#v %v calls=%v", got, err, f.calls)
	}
}
