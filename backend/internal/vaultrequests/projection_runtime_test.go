package vaultrequests

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

func TestRuntimeProjectsMetadataButExecutesAndCompensatesExactValues(t *testing.T) {
	for _, always := range []bool{false, true} {
		for _, failOutput := range []bool{false, true} {
			name := "prompt"
			if always {
				name = "always"
			}
			if failOutput {
				name += " projection failure"
			}
			t.Run(name, func(t *testing.T) {
				harness := newRuntimeHarness(t)
				harness.runAlways = always
				const canary = "PRIVATE_CANARY_1729"
				prepare := harness.runtime.prepare
				harness.runtime.prepare = func(ctx context.Context, tokenID int64, projectRef, action string, input map[string]any) (PreparedAction, error) {
					prepared, err := prepare(ctx, tokenID, projectRef, action, input)
					prepared.ApprovalContext.Items = []projectvault.SessionItem{{ItemID: 42, Name: canary, SourceProjectID: harness.projectID}}
					return prepared, err
				}
				harness.runtime.redactProjection = func(_ context.Context, value any) (any, error) {
					if output, ok := value.(map[string]any); ok && output["environment_names"] != nil && failOutput {
						return nil, errors.New("output projection failed")
					}
					return RedactProjection(value, func(text string) string { return strings.ReplaceAll(text, canary, "[REDACTED]") })
				}
				var executed, compensated bool
				harness.runtime.execute = func(_ context.Context, request Request) (any, error) {
					encoded, err := json.Marshal(request.ApprovalContext)
					if err != nil || !strings.Contains(string(encoded), canary) || request.Input["name"] != canary || request.Reason != canary {
						t.Fatalf("execution did not receive exact metadata: %#v err=%v", request, err)
					}
					executed = true
					return map[string]any{"environment_names": []string{canary}, "session_id": 42}, nil
				}
				harness.runtime.compensate = func(_ context.Context, request Request, output any) error {
					if request.Input["name"] != canary || output.(map[string]any)["environment_names"].([]string)[0] != canary {
						t.Fatal("compensation must receive exact execution values")
					}
					compensated = true
					return nil
				}
				item, err := harness.runtime.Call(t.Context(), CallInput{
					TokenID: harness.tokenID, ProjectRef: harness.projectRef, ActionName: ActionGenerateItem,
					Input: map[string]any{"name": canary, "generator_kind": "random_token"}, Reason: canary, IdempotencyKey: "metadata-test",
				})
				if err != nil {
					t.Fatal(err)
				}
				if !always {
					encoded, err := json.Marshal(item.ApprovalContext)
					if err != nil || strings.Contains(string(encoded), canary) || item.ApprovalContextHash != "approval-hash" {
						t.Fatalf("unsafe approval: %#v err=%v", item, err)
					}
					result, err := harness.runtime.RunPending(t.Context(), item.ID, "approve")
					if err != nil {
						t.Fatal(err)
					}
					item = result.Request
				}
				wantStatus := StatusCompleted
				if failOutput {
					wantStatus = StatusFailed
				}
				if !executed || compensated != failOutput || item.Status != wantStatus {
					t.Fatalf("executed=%v compensated=%v status=%s", executed, compensated, item.Status)
				}
				stored, err := harness.store.Get(t.Context(), item.ID)
				if err != nil {
					t.Fatal(err)
				}
				// The test sealer deliberately wraps plaintext; inspect only public fields.
				stored.EncryptedPayloadJSON = ""
				encoded, err := json.Marshal(stored)
				if err != nil || strings.Contains(string(encoded), canary) {
					t.Fatalf("unsafe public request: %s err=%v", encoded, err)
				}
				if !failOutput && stored.Output.(map[string]any)["session_id"].(float64) != 42 {
					t.Fatal("machine identifier changed")
				}
			})
		}
	}
}
