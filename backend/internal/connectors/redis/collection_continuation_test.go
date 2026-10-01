package redisconnector

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestCollectionContinuationRetainsEveryStableEntry(t *testing.T) {
	const total = 175
	for _, kind := range []string{"hash", "set", "list", "zset"} {
		t.Run(kind, func(t *testing.T) {
			runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
				switch command[0] {
				case "TYPE":
					return "+" + kind + "\r\n"
				case "PTTL":
					return ":-1\r\n"
				}
				start, err := strconv.Atoi(command[2])
				if err != nil {
					t.Fatal(err)
				}
				if kind == "hash" || kind == "set" {
					if command[4] != "100" {
						t.Fatalf("COUNT changed when replaying a page: %#v", command)
					}
					count := min(100, total-start)
					next := "0"
					if start+count < total {
						next = strconv.Itoa(start + count)
					}
					return collectionScanReply(next, largeCollectionPage(kind, start, count))
				}
				end, err := strconv.Atoi(command[3])
				if err != nil {
					t.Fatal(err)
				}
				return collectionArrayReply(largeCollectionPage(kind, start, min(end-start+1, total-start)))
			})
			seen := map[int]bool{}
			input := map[string]any{"key": "stable", "limit": 1000, "max_bytes": 8192}
			complete := false
			for page := 0; page < 10; page++ {
				result, err := Connector{}.ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionGetKey, Payload: input})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := actionresult.Canonicalize(result.Output, actionresult.DefaultLimits()); err != nil {
					t.Fatal(err)
				}
				output := result.Output.(map[string]any)
				collectStablePreviewEntries(t, kind, output["value"], seen)
				complete = output["complete"] == true
				if complete {
					if output["next_input"] != nil {
						t.Fatal("complete result has a continuation")
					}
					break
				}
				// Use the same JSON path as an MCP client, including integer decoding.
				encoded, err := json.Marshal(output["next_input"])
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(encoded, &input); err != nil {
					t.Fatal(err)
				}
			}
			if !complete || len(seen) != total {
				t.Fatalf("continuation lost entries: complete=%v count=%d want=%d", complete, len(seen), total)
			}
		})
	}
}

func collectStablePreviewEntries(t *testing.T, kind string, value any, seen map[int]bool) {
	t.Helper()
	entries := []string{}
	switch kind {
	case "hash":
		for field := range value.(map[string]string) {
			var index int
			if _, err := fmt.Sscanf(field, "field-%d", &index); err != nil {
				t.Fatal(err)
			}
			entries = append(entries, fmt.Sprintf("%08d", index))
		}
	case "zset":
		for _, pair := range value.([]map[string]string) {
			entries = append(entries, pair["member"])
		}
	default:
		entries = value.([]string)
	}
	for _, entry := range entries {
		if len(entry) < 8 {
			t.Fatalf("entry identity was shortened: %q", entry)
		}
		index, err := strconv.Atoi(entry[:8])
		if err != nil || seen[index] {
			t.Fatalf("invalid or duplicated entry: index=%d seen=%v err=%v", index, seen[index], err)
		}
		seen[index] = true
	}
}
