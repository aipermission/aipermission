package redisconnector

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestScanAdvancesCursorWithoutDroppingOverfetch(t *testing.T) {
	for _, test := range []struct {
		name, start string
		first       []string
		limit       int
	}{
		{name: "multiple pages", start: "0", first: []string{"a"}, limit: 2},
		{name: "empty continuation", start: "0", limit: 2},
		{name: "nonzero cursor", start: "3", first: []string{"a"}, limit: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var cursors []string
			runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
				cursors = append(cursors, command[1])
				if len(cursors) == 1 {
					return scanResponse("7", test.first...)
				}
				return scanResponse("0", "b", "c", "d")
			})
			result, err := (Connector{}).ExecuteAction(context.Background(), runtime, connectors.PreparedAction{
				ActionName: ActionScanKeys, Payload: map[string]any{"cursor": test.start, "limit": test.limit},
			})
			if err != nil {
				t.Fatal(err)
			}
			output := result.Output.(map[string]any)
			if _, err := actionresult.Canonicalize(output, actionresult.DefaultLimits()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cursors, []string{test.start, "7"}) {
				t.Fatalf("cursors = %v", cursors)
			}
			want := append(append([]string{}, test.first...), "b", "c", "d")
			if !reflect.DeepEqual(output["keys"], want) || output["next_cursor"] != "0" || output["complete"] != true {
				t.Fatalf("output = %#v; want keys %v", output, want)
			}
		})
	}
}

func TestScanPageBudgetReturnsContinuation(t *testing.T) {
	calls := 0
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		calls++
		return scanResponse(fmt.Sprint(calls))
	})
	result, err := (Connector{}).ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionScanKeys})
	if err != nil {
		t.Fatal(err)
	}
	output := result.Output.(map[string]any)
	if calls != maxScanPages || output["next_cursor"] != fmt.Sprint(maxScanPages) || output["complete"] != false || output["scan_limit_reached"] != true {
		t.Fatalf("calls = %d; output = %#v", calls, output)
	}
}

func TestScanCancellationInterruptsBlockedRead(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	requested := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		close(requested)
		<-release
		return scanResponse("0")
	})
	done := make(chan error, 1)
	go func() {
		_, err := (Connector{}).ExecuteAction(ctx, runtime, connectors.PreparedAction{ActionName: ActionScanKeys})
		done <- err
	}()
	select {
	case <-requested:
	case <-time.After(time.Second):
		t.Fatal("SCAN did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled scan error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt read")
	}
}

func TestScanLimitPreservesNonzeroContinuation(t *testing.T) {
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string { return scanResponse("7", "a", "b") })
	result, err := (Connector{}).ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionScanKeys, Payload: map[string]any{"limit": 1}})
	if err != nil {
		t.Fatal(err)
	}
	output := result.Output.(map[string]any)
	if output["next_cursor"] != "7" || output["complete"] != false || !reflect.DeepEqual(output["keys"], []string{"a", "b"}) {
		t.Fatalf("output = %#v", output)
	}
}

func TestScanRejectsInvalidResponseCursor(t *testing.T) {
	for _, cursor := range []string{"", "-1", "not-a-cursor", "18446744073709551616"} {
		t.Run(cursor, func(t *testing.T) {
			runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string { return scanResponse(cursor) })
			_, err := (Connector{}).ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionScanKeys})
			if err == nil {
				t.Fatal("invalid cursor accepted")
			}
		})
	}
}

func scanResponse(cursor string, keys ...string) string {
	response := "*2\r\n" + respBulk(cursor)
	response += "*" + fmt.Sprint(len(keys)) + "\r\n"
	for _, key := range keys {
		response += respBulk(key)
	}
	return response
}

func TestScanCallerDeadlineDoesNotResetPerPage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		if command[1] == "0" {
			return scanResponse("7")
		}
		<-ctx.Done()
		return scanResponse("7")
	})
	start := time.Now()
	_, err := (Connector{}).ExecuteAction(ctx, runtime, connectors.PreparedAction{ActionName: ActionScanKeys})
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("deadline not honored: %v after %v", err, time.Since(start))
	}
}

func TestScanAlreadyCanceledDoesNotSend(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		t.Error("sent after cancellation")
		return scanResponse("0")
	})
	_, err := (Connector{}).ExecuteAction(ctx, runtime, connectors.PreparedAction{ActionName: ActionScanKeys})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestScanMaximumWholePageFitsOutputBoundary(t *testing.T) {
	keys := make([]string, maxRESPArrayItems)
	for index := range keys {
		keys[index] = fmt.Sprintf("key:%04d", index)
	}
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string { return scanResponse("7", keys...) })
	result, err := (Connector{}).ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionScanKeys, Payload: map[string]any{"limit": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := actionresult.Canonicalize(result.Output, actionresult.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Output.(map[string]any)["keys"], keys) {
		t.Fatal("overfetch lost keys")
	}
}

func TestScanBoundsAccumulatedKeyBytes(t *testing.T) {
	key := strings.Repeat("k", maxRESPBulkBytes)
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string { return scanResponse("7", key) })
	_, err := (Connector{}).ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionScanKeys})
	if err == nil || !strings.Contains(err.Error(), "scan keys exceed") {
		t.Fatalf("error = %v", err)
	}
}

func TestScanBoundsEscapedOutput(t *testing.T) {
	key := strings.Repeat("\x00", maxScanKeyBytes/2)
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string { return scanResponse("0", key) })
	_, err := (Connector{}).ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionScanKeys})
	if err == nil || !strings.Contains(err.Error(), "encoded byte limit") {
		t.Fatalf("error = %v", err)
	}
}

func TestCollectionScanEmptyPagesAreBounded(t *testing.T) {
	calls := 0
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		switch command[0] {
		case "TYPE":
			return "+set\r\n"
		case "PTTL":
			return ":-1\r\n"
		case "SCARD":
			return ":1\r\n"
		case "SSCAN":
			calls++
			return scanResponse("7")
		default:
			return "-ERR unexpected\r\n"
		}
	})
	_, err := (Connector{}).ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionGetKey, Payload: map[string]any{"key": "set"}})
	if err == nil || !strings.Contains(err.Error(), "exceeded") || calls != maxScanPages {
		t.Fatalf("calls %d, error %v", calls, err)
	}
}
