package redisconnector

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestEmptyCollectionValueShapesRemainCompatible(t *testing.T) {
	for kind, expected := range map[string]string{"hash": "{}", "set": "[]", "list": "null", "zset": "[]"} {
		t.Run(kind, func(t *testing.T) {
			runtime := testRuntimeWithScript(t, func(_ *testing.T, command []string) string {
				switch command[0] {
				case "TYPE":
					return "+" + kind + "\r\n"
				case "PTTL":
					return ":-1\r\n"
				default:
					if kind == "hash" || kind == "set" {
						return collectionScanReply("0", nil)
					}
					return collectionArrayReply(nil)
				}
			})
			result, err := Connector{}.ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionGetKey, Payload: map[string]any{"key": "empty"}})
			if err != nil {
				t.Fatal(err)
			}
			output := result.Output.(map[string]any)
			encoded, err := json.Marshal(output["value"])
			if err != nil || string(encoded) != expected || output["complete"] != true || output["truncated"] != false || output["returned_items"] != 0 {
				t.Fatalf("changed empty %s shape: value=%s output=%#v err=%v", kind, encoded, output, err)
			}
		})
	}
}

func TestScanContinuationRejectsChangedShorterPage(t *testing.T) {
	runtime := testRuntimeWithScript(t, func(_ *testing.T, command []string) string {
		switch command[0] {
		case "TYPE":
			return "+hash\r\n"
		case "PTTL":
			return ":-1\r\n"
		default:
			return collectionScanReply("0", []string{"field", "value"})
		}
	})
	result, err := Connector{}.ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionGetKey, Payload: map[string]any{"key": "changed", "cursor": "7", "offset": 2}})
	if err == nil || !strings.Contains(err.Error(), "no longer fits") || result.Status == connectors.ResultCompleted {
		t.Fatalf("changed page became misleading completion: result=%#v err=%v", result, err)
	}
}

func TestRangeContinuationNeverReturnsAnUnusableOffset(t *testing.T) {
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		switch command[0] {
		case "TYPE":
			return "+list\r\n"
		case "PTTL":
			return ":-1\r\n"
		default:
			if command[2] != strconv.FormatInt(maxCollectionRangeOffset, 10) {
				t.Fatalf("range offset changed: %#v", command)
			}
			return collectionArrayReply([]string{"value"})
		}
	})
	result, err := Connector{}.ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionGetKey, Payload: map[string]any{"key": "large", "offset": maxCollectionRangeOffset, "limit": 1}})
	if err == nil || !strings.Contains(err.Error(), "supported range") || result.Status == connectors.ResultCompleted {
		t.Fatalf("returned unusable continuation: result=%#v err=%v", result, err)
	}
}
