package vaultrequests

import (
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

func TestNormalizeActionInputRejectsUnknownFieldsAndCanonicalizesPayload(t *testing.T) {
	if _, err := NormalizeActionInput(ActionGenerateItem, map[string]any{
		"name": "GENERATED_KEY", "generator_kind": "hex_secret", "unexpected": true,
	}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error = %v", err)
	}
	generated, err := NormalizeActionInput(ActionGenerateItem, map[string]any{
		"name": "GENERATED_KEY", "generator_kind": "hex_secret",
	})
	if err != nil || generated["secret_type"] != projectvault.DefaultSecretType {
		t.Fatalf("normalized generated input = %#v error=%v", generated, err)
	}
	normalized, err := NormalizeActionInput(ActionRestartSession, map[string]any{
		"target_ref": "ssh:1:2",
		"items":      []any{map[string]any{"item_id": 3, "source_project_id": 4}},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, ok := normalized["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("normalized items = %#v", normalized["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok || item["item_id"] != float64(3) || item["source_project_id"] != float64(4) {
		t.Fatalf("normalized item = %#v", items[0])
	}
	if _, exists := item["replace_existing"]; exists {
		t.Fatalf("omitempty field unexpectedly persisted: %#v", item)
	}
}

func TestActionInputsProjectIntoVaultDomainTypes(t *testing.T) {
	generate := GenerateInput{UsageNotes: []UsageNoteInput{{Location: "service.env", Notes: "runtime"}}}
	notes := generate.ProjectUsageNotes()
	if len(notes) != 1 || notes[0].Location != "service.env" || notes[0].Notes != "runtime" {
		t.Fatalf("usage notes = %#v", notes)
	}
	apply := SessionApplyInput{Items: []SessionSelectionInput{{ItemID: 7, SourceProjectID: 9, ReplaceExisting: true}}}
	selections := apply.SessionSelections()
	if len(selections) != 1 || selections[0].ItemID != 7 || selections[0].SourceProjectID != 9 || !selections[0].ReplaceExisting {
		t.Fatalf("session selections = %#v", selections)
	}
}

func TestDecodeApprovalContextIsStrict(t *testing.T) {
	approval, err := DecodeApprovalContext(map[string]any{
		"schema": ApprovalContextSchema, "token_id": 7, "project_id": 9,
	})
	if err != nil || approval.Schema != ApprovalContextSchema || approval.TokenID != 7 || approval.ProjectID != 9 {
		t.Fatalf("approval = %#v error=%v", approval, err)
	}
	if _, err := DecodeApprovalContext(map[string]any{
		"schema": ApprovalContextSchema, "unknown_security_field": true,
	}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown approval field error = %v", err)
	}
}

func TestDecodeActionInputsAreStrict(t *testing.T) {
	generated, err := DecodeGenerateInput(map[string]any{
		"name": "DEPLOY_TOKEN", "generator_kind": "hex_secret",
		"usage_notes": []any{map[string]any{"location": "service.env"}},
	})
	if err != nil || generated.Name != "DEPLOY_TOKEN" || len(generated.UsageNotes) != 1 {
		t.Fatalf("generated = %#v error=%v", generated, err)
	}
	if _, err := DecodeGenerateInput(map[string]any{"unexpected": true}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown generate field error = %v", err)
	}

	session, err := DecodeSessionApplyInput(map[string]any{
		"target_ref": "ssh:1:2",
		"items":      []any{map[string]any{"item_id": 3, "source_project_id": 4, "replace_existing": true}},
	})
	if err != nil || session.TargetRef != "ssh:1:2" || len(session.Items) != 1 || !session.Items[0].ReplaceExisting {
		t.Fatalf("session = %#v error=%v", session, err)
	}
	if _, err := DecodeSessionApplyInput(map[string]any{"unexpected": true}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown session field error = %v", err)
	}
}
