package redisconnector

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestRedisKeySchemasPreserveWhitespace(t *testing.T) {
	actions, err := (Connector{}).GetActionList(context.Background(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatal(err)
	}
	for _, actionName := range []string{ActionGetKey, ActionSetString, ActionExpireKey} {
		action := actionByName(t, actions, actionName)
		var keyField connectors.Field
		for _, field := range action.InputSchema.Fields {
			if field.Name == "key" {
				keyField = field
				break
			}
		}
		if !keyField.PreserveWhitespace {
			t.Fatalf("%s key field does not preserve whitespace", actionName)
		}
		normalized, err := connectors.NormalizeSchemaValues(action.InputSchema, redisIdentityInput(actionName, " \t\n"))
		if err != nil {
			t.Fatalf("normalize %s: %v", actionName, err)
		}
		if normalized["key"] != " \t\n" {
			t.Fatalf("%s key = %q", actionName, normalized["key"])
		}
	}
}

func TestRedisKeyIdentityPreservedThroughPreparationAndExecution(t *testing.T) {
	const key = " \tjob\n "
	tests := []struct {
		name       string
		actionName string
		input      map[string]any
		response   string
		want       []string
	}{
		{name: "set", actionName: ActionSetString, input: map[string]any{"key": key, "value": "ready"}, response: "+OK\r\n", want: []string{"SET", key, "ready"}},
		{name: "expire", actionName: ActionExpireKey, input: map[string]any{"key": key, "ttl_seconds": 60}, response: ":1\r\n", want: []string{"EXPIRE", key, "60"}},
		{name: "delete", actionName: ActionDeleteKeys, input: map[string]any{"keys": []any{key, "job", key}}, response: ":1\r\n", want: []string{"DEL", key, "job"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prepared, err := (Connector{}).PrepareAction(context.Background(), connectors.ActionRequest{
				Target:     connectors.TargetView{Name: "cache"},
				Profile:    connectors.CredentialProfileView{Label: "default"},
				ActionName: test.actionName,
				Input:      test.input,
			})
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
				if !reflect.DeepEqual(command, test.want) {
					t.Fatalf("command = %#v, want %#v", command, test.want)
				}
				return test.response
			})
			if _, err := (Connector{}).ExecuteAction(context.Background(), runtime, prepared); err != nil {
				t.Fatalf("execute: %v", err)
			}
		})
	}
}

func TestRedisGetPreservesExactKeyIdentity(t *testing.T) {
	const key = " \tjob\n "
	commands := [][]string{}
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		commands = append(commands, append([]string(nil), command...))
		switch command[0] {
		case "TYPE":
			return "+none\r\n"
		case "PTTL":
			return ":-2\r\n"
		default:
			t.Fatalf("unexpected command = %#v", command)
			return ""
		}
	})
	prepared, err := (Connector{}).PrepareAction(context.Background(), connectors.ActionRequest{
		Target: connectors.TargetView{Name: "cache"}, Profile: connectors.CredentialProfileView{Label: "default"},
		ActionName: ActionGetKey, Input: map[string]any{"key": key},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := (Connector{}).ExecuteAction(context.Background(), runtime, prepared); err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := [][]string{{"TYPE", key}, {"PTTL", key}}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestRedisAuthenticationPreservesPasswordBytes(t *testing.T) {
	for _, test := range []struct {
		name     string
		password string
	}{
		{name: "padded", password: " \tsecret\n "},
		{name: "whitespace only", password: " \t\n "},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
				if !reflect.DeepEqual(command, []string{"AUTH", test.password}) {
					t.Fatalf("command = %#v", command)
				}
				return "+OK\r\n"
			})
			runtime.Secrets = testSecrets{"password": test.password}
			client, err := openRedisClient(context.Background(), runtime)
			if err != nil {
				t.Fatalf("open client: %v", err)
			}
			client.Close()
		})
	}
}

func TestRedisAuthenticationDistinguishesMissingAndEmptyPassword(t *testing.T) {
	runtime := testRuntimeWithScript(t, func(t *testing.T, command []string) string {
		t.Fatalf("unexpected command = %#v", command)
		return ""
	})
	client, err := openRedisClient(context.Background(), runtime)
	if err != nil {
		t.Fatalf("missing optional password: %v", err)
	}
	client.Close()

	runtime.Secrets = testSecrets{"password": ""}
	if _, err := openRedisClient(context.Background(), runtime); !errors.Is(err, ErrMissingSecret) {
		t.Fatalf("empty stored password error = %v", err)
	}
}

func TestNormalizeRedisKeysRejectsNonStringValues(t *testing.T) {
	if _, err := normalizeKeys([]any{"key", 12}); err == nil {
		t.Fatal("non-string key accepted")
	}
}

func actionByName(t *testing.T, actions []connectors.ActionDefinition, name string) connectors.ActionDefinition {
	t.Helper()
	for _, action := range actions {
		if action.Name == name {
			return action
		}
	}
	t.Fatalf("action %q not found", name)
	return connectors.ActionDefinition{}
}

func redisIdentityInput(actionName, key string) map[string]any {
	input := map[string]any{"key": key}
	switch actionName {
	case ActionSetString:
		input["value"] = "value"
	case ActionExpireKey:
		input["ttl_seconds"] = 60
	}
	return input
}
