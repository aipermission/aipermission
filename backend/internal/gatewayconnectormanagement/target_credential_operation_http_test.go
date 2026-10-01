package gatewayconnectormanagement

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestCredentialOperationInputCanonicalInt64String(t *testing.T) {
	for _, scenario := range []struct {
		text string
		want int64
	}{
		{"1", 1}, {"7", 7}, {"9007199254740993", 9007199254740993},
		{"9223372036854775807", 9223372036854775807},
	} {
		t.Run(scenario.text, func(t *testing.T) {
			input := map[string]any{"mode": "inspect", "nested": map[string]any{"value": true}}
			id, got, ok := credentialOperationInput(map[string]any{"profile_id": scenario.text, "input": input})
			if !ok || id != scenario.want || !reflect.DeepEqual(got, input) {
				t.Fatalf("id=%d input=%#v ok=%t", id, got, ok)
			}
		})
	}
	if id, input, ok := credentialOperationInput(map[string]any{"profile_id": "1", "input": map[string]any{}}); !ok || id != 1 || input == nil || len(input) != 0 {
		t.Fatalf("empty input object rejected: id=%d input=%#v ok=%t", id, input, ok)
	}
}

func TestCredentialOperationInputRejectsNoncanonicalAndWrongShapes(t *testing.T) {
	for _, value := range []any{
		"", "0", "-0", "-1", "01", "0001", "+1", " 1", "1 ", "\t1", "1\n",
		"1.0", "1e0", "0x1", "1_000", "NaN", "Inf", "one", "1\x00", "\u0661", "\uff11",
		"9223372036854775808", "-9223372036854775809", "999999999999999999999999999999999999",
		nil, true, int(1), int64(1), float64(1), json.Number("1"), []any{"1"}, map[string]any{"id": "1"},
	} {
		t.Run(fmt.Sprintf("profile/%T/%q", value, fmt.Sprint(value)), func(t *testing.T) {
			if _, _, ok := credentialOperationInput(map[string]any{"profile_id": value, "input": map[string]any{}}); ok {
				t.Fatalf("accepted noncanonical profile id %#v", value)
			}
		})
	}
	for _, scenario := range []struct {
		name    string
		request map[string]any
	}{
		{"nil request", nil}, {"empty request", map[string]any{}},
		{"missing profile", map[string]any{"input": map[string]any{}}},
		{"missing input", map[string]any{"profile_id": "1"}},
		{"unknown instead of input", map[string]any{"profile_id": "1", "unknown": map[string]any{}}},
		{"unknown instead of profile", map[string]any{"unknown": "1", "input": map[string]any{}}},
		{"extra field", map[string]any{"profile_id": "1", "input": map[string]any{}, "unknown": true}},
		{"nil input", map[string]any{"profile_id": "1", "input": nil}},
		{"typed nil input", map[string]any{"profile_id": "1", "input": map[string]any(nil)}},
		{"array input", map[string]any{"profile_id": "1", "input": []any{}}},
		{"string input", map[string]any{"profile_id": "1", "input": "{}"}},
		{"boolean input", map[string]any{"profile_id": "1", "input": false}},
		{"numeric input", map[string]any{"profile_id": "1", "input": float64(1)}},
		{"wrong map input", map[string]any{"profile_id": "1", "input": map[string]string{}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if _, _, ok := credentialOperationInput(scenario.request); ok {
				t.Fatalf("accepted invalid request %#v", scenario.request)
			}
		})
	}
}

type credentialOperationAuditProbe struct {
	targetDraftPeer
	required   func(context.Context, string, any) error
	bestEffort func(context.Context, string, *int64, int64, string, any)
}

func (probe credentialOperationAuditProbe) ConnectorWriteTargetAudit(ctx context.Context, action string, payload any) error {
	return probe.required(ctx, action, payload)
}

func (probe credentialOperationAuditProbe) ConnectorWriteAudit(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	probe.bestEffort(ctx, actor, tokenID, runtimeID, action, payload)
}

func credentialOperationTestRuntime(t *testing.T) connectormanagement.CredentialRuntimePorts {
	t.Helper()
	identity := func(_ context.Context, value string) string { return value }
	redactor, err := actionresult.NewRedactor(identity, identity, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return connectormanagement.RuntimeCredentialPorts(connectormanagement.CredentialStorage{}, nil,
		func(ctx context.Context, result connectors.ActionResult, boundary connectormanagement.CredentialBoundary) (connectors.ActionResult, error) {
			return redactor.ResultWithCredentialBoundary(ctx, result, boundary)
		}, identity)
}

func TestCredentialOperationGatewayProjectsBeforeAuditDelegation(t *testing.T) {
	for _, required := range []bool{true, false} {
		t.Run(map[bool]string{true: "required", false: "best effort"}[required], func(t *testing.T) {
			boundary := actionresult.NewCredentialBoundary(map[string]any{
				"password": "known-password", "unused": "unread-password",
				"nested": []any{map[string]any{"value": "nested-password"}},
			})
			encoded := base64.StdEncoding.EncodeToString([]byte("known-password"))
			payload := map[string]any{"summary": "known-password unread-password nested-password derived-password " + encoded, "keep": "visible"}
			steps := []string{}
			ctx := t.Context()
			tokenID := int64(19)
			delegateFailure := errors.New("delegate audit failure")
			check := func(gotCtx context.Context, action string, projected any) {
				if gotCtx != ctx || action != "credential.inspect" || !reflect.DeepEqual(steps, []string{"project"}) {
					t.Fatalf("audit lost context/action or ran before projection: %q %v", action, steps)
				}
				output := fmt.Sprint(projected)
				for _, secret := range []string{"known-password", "unread-password", "nested-password", "derived-password", encoded} {
					if strings.Contains(output, secret) {
						t.Fatalf("audit delegate received secret %q: %s", secret, output)
					}
				}
				if projected.(map[string]any)["keep"] != "visible" {
					t.Fatal("projection lost nonsensitive audit payload")
				}
				steps = append(steps, "delegate")
			}
			probe := credentialOperationAuditProbe{
				required: func(gotCtx context.Context, action string, projected any) error {
					if !required {
						t.Fatal("best effort audit reached required delegate")
					}
					check(gotCtx, action, projected)
					return delegateFailure
				},
				bestEffort: func(gotCtx context.Context, actor string, gotToken *int64, runtimeID int64, action string, projected any) {
					if required || actor != "local" || gotToken != &tokenID || runtimeID != 23 {
						t.Fatal("best effort audit lost routing metadata")
					}
					check(gotCtx, action, projected)
				},
			}
			runtime := credentialOperationTestRuntime(t)
			project := runtime.RedactResult
			runtime.RedactResult = func(gotCtx context.Context, result connectors.ActionResult, gotBoundary connectormanagement.CredentialBoundary) (connectors.ActionResult, error) {
				if gotCtx != ctx || !reflect.DeepEqual(result.Output, payload) || !gotBoundary.Valid() {
					t.Fatal("audit projection lost payload, context or boundary")
				}
				steps = append(steps, "project")
				return project(gotCtx, result, gotBoundary)
			}
			gateway := credentialOperationGateway{TargetOperationGateway: probe, runtime: runtime, boundary: boundary}
			// Registration after gateway construction must update its shared boundary.
			boundary.Add("derived-password")
			if required {
				if err := gateway.ConnectorWriteTargetAudit(ctx, "credential.inspect", payload); !errors.Is(err, delegateFailure) {
					t.Fatalf("required audit lost delegate error: %v", err)
				}
			} else {
				gateway.ConnectorWriteAudit(ctx, "local", &tokenID, 23, "credential.inspect", payload)
			}
			if !reflect.DeepEqual(steps, []string{"project", "delegate"}) || !strings.Contains(payload["summary"].(string), "known-password") {
				t.Fatalf("steps=%v source payload=%#v", steps, payload)
			}
		})
	}
}

func TestCredentialOperationGatewayProjectionFailureNeverAudits(t *testing.T) {
	for _, scenario := range []string{"missing projector", "invalid boundary", "projector error", "unsupported payload"} {
		t.Run(scenario, func(t *testing.T) {
			runtime := credentialOperationTestRuntime(t)
			boundary := actionresult.NewCredentialBoundary(map[string]any{"password": "private-projection-secret"})
			var payload any = map[string]any{"summary": "private-projection-secret"}
			projectCalls := 0
			project := runtime.RedactResult
			runtime.RedactResult = func(ctx context.Context, result connectors.ActionResult, boundary connectormanagement.CredentialBoundary) (connectors.ActionResult, error) {
				projectCalls++
				if scenario == "projector error" {
					return connectors.ActionResult{Output: payload}, errors.New("private-projection-secret")
				}
				return project(ctx, result, boundary)
			}
			switch scenario {
			case "missing projector":
				runtime.RedactResult = nil
			case "invalid boundary":
				boundary = connectormanagement.CredentialBoundary{}
			case "unsupported payload":
				payload = make(chan string)
			}
			probe := credentialOperationAuditProbe{
				required: func(context.Context, string, any) error {
					t.Fatal("required audit delegated after projection failure")
					return nil
				},
				bestEffort: func(context.Context, string, *int64, int64, string, any) {
					t.Fatal("best effort audit delegated after projection failure")
				},
			}
			gateway := credentialOperationGateway{TargetOperationGateway: probe, runtime: runtime, boundary: boundary}
			if err := gateway.ConnectorWriteTargetAudit(t.Context(), "inspect", payload); err == nil || strings.Contains(err.Error(), "private-projection-secret") {
				t.Fatalf("required audit did not fail closed: %v", err)
			}
			gateway.ConnectorWriteAudit(t.Context(), "local", nil, 0, "inspect", payload)
			wantCalls := 2
			if scenario == "missing projector" || scenario == "invalid boundary" {
				wantCalls = 0
			}
			if projectCalls != wantCalls {
				t.Fatalf("projector calls=%d want=%d", projectCalls, wantCalls)
			}
		})
	}
}
