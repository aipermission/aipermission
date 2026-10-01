package redisconnector

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestCollectionPreviewSurvivesSharedResultProjection(t *testing.T) {
	for _, kind := range []string{"hash", "set", "list", "zset"} {
		t.Run(kind, func(t *testing.T) {
			requests := 0
			runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
				switch command[0] {
				case "TYPE":
					return "+" + kind + "\r\n"
				case "PTTL":
					return ":-1\r\n"
				}
				requests++
				if kind == "hash" || kind == "set" {
					cursor, err := strconv.Atoi(command[2])
					if err != nil {
						t.Fatal(err)
					}
					return collectionScanReply(strconv.Itoa(cursor+1), largeCollectionPage(kind, cursor*100, 100))
				}
				return collectionArrayReply(largeCollectionPage(kind, 0, 900))
			})
			result, err := Connector{}.ExecuteAction(t.Context(), runtime, connectors.PreparedAction{
				ActionName: ActionGetKey, Payload: map[string]any{"key": "large", "limit": 1000, "max_bytes": 8192},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := actionresult.Canonicalize(result.Output, actionresult.DefaultLimits()); err != nil {
				t.Fatalf("usable collection preview lost at shared boundary: %v", err)
			}
			output := result.Output.(map[string]any)
			encoded, err := json.Marshal(output["value"])
			if err != nil || len(encoded) > maxValueBytes || output["truncated"] != true || output["complete"] != false || output["next_input"] == nil {
				t.Fatalf("unbounded or misleading preview: bytes=%d truncated=%v complete=%v next=%v err=%v", len(encoded), output["truncated"], output["complete"], output["next_input"], err)
			}
			if requests != 1 || len(result.DisplayText) > 4000 {
				t.Fatalf("continued reading after budget exhaustion: requests=%d display=%d", requests, len(result.DisplayText))
			}
		})
	}
}

func largeCollectionPage(kind string, start, count int) []string {
	items := make([]string, 0, count*2)
	for index := start; index < start+count; index++ {
		value := fmt.Sprintf("%08d", index) + strings.Repeat("v", 8192-8)
		switch kind {
		case "hash":
			items = append(items, fmt.Sprintf("field-%d", index), value)
		case "zset":
			items = append(items, value, "1")
		default:
			items = append(items, value)
		}
	}
	return items
}

func collectionArrayReply(items []string) string {
	var reply strings.Builder
	fmt.Fprintf(&reply, "*%d\r\n", len(items))
	for _, item := range items {
		reply.WriteString(respBulk(item))
	}
	return reply.String()
}

func collectionScanReply(cursor string, items []string) string {
	return "*2\r\n" + respBulk(cursor) + collectionArrayReply(items)
}
