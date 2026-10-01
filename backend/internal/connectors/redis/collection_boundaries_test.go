package redisconnector

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestCollectionBudgetIncludesEscapedValuesAndFieldNames(t *testing.T) {
	for _, kind := range []string{"hash", "set", "list", "zset"} {
		t.Run(kind, func(t *testing.T) {
			preview := newCollectionPreview(kind, maxValueBytes)
			entry := []string{strings.Repeat("\x00", maxValueBytes)}
			if kind == "hash" {
				entry = append([]string{"\x00<field>"}, entry...)
			} else if kind == "zset" {
				entry = append(entry, "-inf")
			}
			if accepted, err := preview.appendEntry(entry); err != nil || !accepted {
				t.Fatalf("first value did not fit as a bounded preview: accepted=%v err=%v", accepted, err)
			}
			encoded, err := json.Marshal(preview.value())
			if err != nil || len(encoded) > maxCollectionPreviewBytes || len(encoded) > preview.encodedBytes || !preview.truncated {
				t.Fatalf("escaped preview escaped budget: encoded=%d accounted=%d truncated=%v err=%v", len(encoded), preview.encodedBytes, preview.truncated, err)
			}
			if _, err := actionresult.Canonicalize(preview.value(), actionresult.DefaultLimits()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHashCollectorClipsBeforeRetainingAndAccountsForDuplicates(t *testing.T) {
	preview := newCollectionPreview("hash", 1)
	for index := 0; index < 24; index++ {
		if accepted, err := preview.appendEntry([]string{"same\x00field", strings.Repeat("v", maxValueBytes)}); err != nil || !accepted {
			t.Fatalf("duplicate collection failed: accepted=%v err=%v", accepted, err)
		}
		encoded, err := json.Marshal(preview.fields)
		if err != nil || preview.count != 1 || len(preview.fields["same\x00field"]) > 1 || preview.encodedBytes != len(encoded)+1 {
			t.Fatalf("retained raw bytes or duplicated accounting: count=%d value=%d encoded=%d accounted=%d err=%v", preview.count, len(preview.fields["same\x00field"]), len(encoded), preview.encodedBytes, err)
		}
	}
}

func TestHashFieldIdentitiesCannotBeSilentlyShortenedOrRepaired(t *testing.T) {
	for _, field := range []string{strings.Repeat("\x00", maxCollectionPreviewBytes/2), string([]byte{0xff})} {
		preview := newCollectionPreview("hash", 1)
		if accepted, err := preview.appendEntry([]string{field, "value"}); err == nil || accepted || preview.count != 0 || len(preview.fields) != 0 {
			t.Fatalf("unrepresentable field became a different identity: accepted=%v err=%v count=%d", accepted, err, preview.count)
		}
	}
}

func TestCollectionContinuationValidationPreventsKeyDispatch(t *testing.T) {
	for _, input := range []map[string]any{
		{"key": "key", "cursor": "-1"},
		{"key": "key", "cursor": 1},
		{"key": "key", "cursor": "18446744073709551616"},
		{"key": "key", "offset": -1},
		{"key": "key", "offset": 1.5},
		{"key": "key", "offset": json.Number("9223372036854775807")},
	} {
		calls := 0
		runtime := testRuntimeWithScript(t, func(*testing.T, []string) string { calls++; return "+OK\r\n" })
		if _, err := (Connector{}).PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionGetKey, Input: input}); err == nil {
			t.Fatalf("prepare accepted invalid continuation: %#v", input)
		}
		if _, err := (Connector{}).ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionGetKey, Payload: input}); err == nil || calls != 0 {
			t.Fatalf("invalid continuation dispatched key commands: input=%#v calls=%d err=%v", input, calls, err)
		}
	}
}

func TestCollectionRangeOffsetDoesNotOverflow(t *testing.T) {
	position, err := collectionPreviewPosition(map[string]any{"offset": json.Number("9223372036854774807")})
	if err != nil || position.offset != math.MaxInt64-maxValueLimit {
		t.Fatalf("last safe offset failed: %#v err=%v", position, err)
	}
	if _, err := collectionPreviewPosition(map[string]any{"offset": json.Number("9223372036854774808")}); err == nil {
		t.Fatal("unsafe range end accepted")
	}
}

func TestCollectionReadCancellationCannotBecomePartialSuccess(t *testing.T) {
	for _, kind := range []string{"hash", "set", "list", "zset"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			runtime := testRuntimeWithScript(t, func(_ *testing.T, command []string) string {
				switch command[0] {
				case "TYPE":
					return "+" + kind + "\r\n"
				case "PTTL":
					return ":-1\r\n"
				default:
					cancel()
					<-ctx.Done()
					return ""
				}
			})
			result, err := Connector{}.ExecuteAction(ctx, runtime, connectors.PreparedAction{ActionName: ActionGetKey, Payload: map[string]any{"key": "key"}})
			if !errors.Is(err, context.Canceled) || result.Status == connectors.ResultCompleted {
				t.Fatalf("canceled collection became success: result=%#v err=%v", result, err)
			}
		})
	}
}
