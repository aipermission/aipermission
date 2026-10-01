package redisconnector

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestScanRejectsBinaryKeysBeforeJSONProjection(t *testing.T) {
	for _, binary := range []string{"\xff", "\xe2\x82", "\xc0\xaf", "\xed\xa0\x80"} {
		for _, laterPage := range []bool{false, true} {
			t.Run(fmt.Sprintf("%x/later=%t", []byte(binary), laterPage), func(t *testing.T) {
				calls := 0
				runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
					calls++
					if command[0] != "SCAN" {
						t.Fatalf("unexpected command: %#v", command)
					}
					if laterPage && calls == 1 {
						return scanResponse("7", "valid-first-page")
					}
					return scanResponse("0", "\ufffd", binary, "valid-sibling")
				})
				result, err := (Connector{}).ExecuteAction(t.Context(), runtime, connectors.PreparedAction{
					ActionName: ActionScanKeys, Payload: map[string]any{"limit": 10},
				})
				if err == nil || !strings.Contains(err.Error(), "UTF-8") {
					t.Fatalf("scan must reject an unrepresentable key, result=%#v, error=%v", result, err)
				}
				if result.Output != nil || result.DisplayText != "" || result.Status != "" {
					t.Fatalf("failed scan published a partial or repaired identity: %#v", result)
				}
			})
		}
	}
}

func TestRedisRejectsBinaryKeyInputsBeforeCommands(t *testing.T) {
	const binary = "secret-key:\xff"
	for _, name := range []string{ActionGetKey, ActionSetString, ActionExpireKey, ActionDeleteKeys} {
		t.Run(name, func(t *testing.T) {
			input := redisIdentityInput(name, binary)
			if name == ActionDeleteKeys {
				input = map[string]any{"keys": []any{"valid-sibling", binary}}
			}
			prepared, err := (Connector{}).PrepareAction(t.Context(), connectors.ActionRequest{ActionName: name, Input: input})
			if err == nil || !strings.Contains(err.Error(), "UTF-8") {
				t.Fatalf("prepare accepted a binary key: %#v / %v", prepared, err)
			}
			if strings.Contains(err.Error(), "secret-key") {
				t.Fatal("key content leaked in validation error")
			}
			runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
				t.Errorf("invalid key reached RESP dispatch: %#v", command)
				return "+OK\r\n"
			})
			result, err := (Connector{}).ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: name, Payload: input})
			if err == nil || !strings.Contains(err.Error(), "UTF-8") || result.Output != nil {
				t.Fatalf("execute accepted bypassed binary key: %#v / %v", result, err)
			}
		})
	}
}

func TestRedisScanJSONIdentitiesRemainExactForEveryKeyAction(t *testing.T) {
	keys := []string{"\ufffd", " \t\n ", "key\x00suffix", "\u65e5\u672c\u8a9e", "\u00e9", "e\u0301"}
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		if command[0] != "SCAN" {
			t.Fatalf("unexpected scan command: %#v", command)
		}
		return scanResponse("0", keys...)
	})
	result, err := (Connector{}).ExecuteAction(t.Context(), runtime, connectors.PreparedAction{ActionName: ActionScanKeys})
	if err != nil {
		t.Fatal(err)
	}
	projected, err := actionresult.Canonicalize(result.Output, actionresult.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	var reply struct{ Keys []string }
	if err := json.Unmarshal(encoded, &reply); err != nil {
		t.Fatal(err)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(reply.Keys, keys) {
		t.Fatalf("JSON projection changed key identities: %#v, want %#v", reply.Keys, keys)
	}
	for _, key := range reply.Keys {
		for _, name := range []string{ActionGetKey, ActionSetString, ActionExpireKey, ActionDeleteKeys} {
			t.Run(name+"/"+key, func(t *testing.T) { assertRedisWireKey(t, name, key) })
		}
	}
}

func TestRedisKeyIdentityByteBoundary(t *testing.T) {
	key := strings.Repeat("k", maxRESPBulkBytes)
	if err := validateRedisKeyIdentity(key); err != nil {
		t.Fatalf("exact byte boundary rejected: %v", err)
	}
	for _, candidate := range []string{key, key + "k"} {
		_, singleErr := exactRedisKey(map[string]any{"key": candidate}, "key")
		_, batchErr := normalizeKeys([]string{candidate})
		wantError := len(candidate) > maxRESPBulkBytes
		if (singleErr != nil) != wantError || (batchErr != nil) != wantError {
			t.Fatalf("byte boundary changed: length=%d, single=%v, batch=%v", len(candidate), singleErr, batchErr)
		}
	}
}

func assertRedisWireKey(t *testing.T, name, key string) {
	t.Helper()
	input := redisIdentityInput(name, key)
	if name == ActionDeleteKeys {
		input = map[string]any{"keys": []any{key}}
	}
	actions, err := (Connector{}).GetActionList(t.Context(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := connectors.NormalizeSchemaValues(actionByName(t, actions, name).InputSchema, input)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := (Connector{}).PrepareAction(t.Context(), connectors.ActionRequest{ActionName: name, Input: normalized})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	var decoded connectors.PreparedAction
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		calls++
		if len(command) < 2 || command[1] != key {
			t.Fatalf("wire identity changed: %#v, want %q", command, key)
		}
		switch command[0] {
		case "TYPE":
			return "+none\r\n"
		case "PTTL":
			return ":-2\r\n"
		case "SET":
			return "+OK\r\n"
		case "EXPIRE", "DEL":
			return ":1\r\n"
		default:
			t.Fatalf("unexpected wire command: %#v", command)
			return ""
		}
	})
	if _, err := (Connector{}).ExecuteAction(t.Context(), runtime, decoded); err != nil {
		t.Fatal(err)
	}
	wantCalls := 1
	if name == ActionGetKey {
		wantCalls = 2
	}
	if calls != wantCalls {
		t.Fatalf("got %d commands, want %d", calls, wantCalls)
	}
}
