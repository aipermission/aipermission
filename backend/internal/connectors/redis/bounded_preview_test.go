package redisconnector

import (
	"context"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestStringPreviewReadsAtMostRequestedBytes(t *testing.T) {
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		switch command[0] {
		case "TYPE":
			return "+string\r\n"
		case "PTTL":
			return ":-1\r\n"
		case "GETRANGE":
			if !reflect.DeepEqual(command, []string{"GETRANGE", "large", "0", "3"}) {
				t.Fatalf("command = %#v", command)
			}
			return respBulk("abcd")
		default:
			t.Fatalf("unexpected command = %#v", command)
			return ""
		}
	})
	result, err := Connector{}.ExecuteAction(context.Background(), runtime, connectors.PreparedAction{
		ActionName: ActionGetKey,
		Payload:    map[string]any{"key": "large", "limit": 1, "max_bytes": 3},
	})
	if err != nil {
		t.Fatalf("get key: %v", err)
	}
	output := result.Output.(map[string]any)
	if output["truncated"] != true || output["value"] == "abcd" {
		t.Fatalf("output = %#v", output)
	}
}

func TestHashPreviewUsesIncrementalScan(t *testing.T) {
	requests := 0
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		switch command[0] {
		case "TYPE":
			return "+hash\r\n"
		case "PTTL":
			return ":-1\r\n"
		case "HSCAN":
			requests++
			if !reflect.DeepEqual(command, []string{"HSCAN", "large", "0", "COUNT", "1"}) {
				t.Fatalf("command = %#v", command)
			}
			return "*2\r\n$1\r\n1\r\n*2\r\n$5\r\nfield\r\n$5\r\nvalue\r\n"
		default:
			t.Fatalf("unexpected command = %#v", command)
			return ""
		}
	})
	result, err := Connector{}.ExecuteAction(context.Background(), runtime, connectors.PreparedAction{
		ActionName: ActionGetKey,
		Payload:    map[string]any{"key": "large", "limit": 1, "max_bytes": 3},
	})
	if err != nil {
		t.Fatalf("get key: %v", err)
	}
	output := result.Output.(map[string]any)
	fields := output["value"].(map[string]string)
	if requests != 1 || fields["field"] == "value" || len(fields) != 1 {
		t.Fatalf("requests=%d value=%v", requests, fields)
	}
}

func TestHashPreviewStopsOnScanLimit(t *testing.T) {
	requests := 0
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		switch command[0] {
		case "TYPE":
			return "+hash\r\n"
		case "PTTL":
			return ":-1\r\n"
		case "HSCAN":
			requests++
			return "*2\r\n$1\r\n1\r\n*0\r\n"
		default:
			t.Fatalf("unexpected command = %#v", command)
			return ""
		}
	})
	_, err := Connector{}.ExecuteAction(context.Background(), runtime, connectors.PreparedAction{
		ActionName: ActionGetKey,
		Payload:    map[string]any{"key": "large", "limit": 1},
	})
	if err == nil || requests != maxScanPages {
		t.Fatalf("requests=%d error=%v", requests, err)
	}
}
