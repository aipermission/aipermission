package rolejournal

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRoleReferenceSurvivesLifecycleConfirmation(t *testing.T) {
	journal := New(newMemoryStore())
	entry := provisionTest(t, journal)
	reference := entry.Reference()
	encoded, err := json.Marshal(reference)
	if err != nil {
		t.Fatal(err)
	}
	var public map[string]any
	if err := json.Unmarshal(encoded, &public); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseReference(public)
	if err != nil || parsed != reference {
		t.Fatalf("reference=%#v error=%v", parsed, err)
	}
	cleanup, _, err := journal.BeginCleanup(t.Context(), entry)
	if err != nil {
		t.Fatal(err)
	}
	cleaned, err := journal.ConfirmCleanup(t.Context(), cleanup)
	if err != nil {
		t.Fatal(err)
	}
	got, err := journal.ResolveReference(t.Context(), reference)
	if err != nil || got != cleaned {
		t.Fatalf("immutable reference lost confirmation: %#v %v", got, err)
	}
}

func TestRoleReferenceRejectsMalformedOrDifferentDurableIdentity(t *testing.T) {
	journal := New(newMemoryStore())
	entry := provisionTest(t, journal)
	for name, mutate := range map[string]func(*Reference){
		"resource zero":         func(r *Reference) { r.ResourceID = "0" },
		"resource leading zero": func(r *Reference) { r.ResourceID = "01" },
		"resource absent":       func(r *Reference) { r.ResourceID = "2" },
		"resource overflow":     func(r *Reference) { r.ResourceID = "9223372036854775808" },
		"role zero":             func(r *Reference) { r.RoleOID = 0 },
		"role successor":        func(r *Reference) { r.RoleOID = r.Intent.Anchor.SuccessorOID },
		"role changed":          func(r *Reference) { r.RoleOID++ },
		"operation":             func(r *Reference) { r.Intent.OperationID = "1234567890abcdef1234567890abcdef" },
		"anchor":                func(r *Reference) { r.Intent.Anchor.DatabaseOID++ },
		"invalid intent":        func(r *Reference) { r.Intent.OperationID = "invalid" },
	} {
		t.Run(name, func(t *testing.T) {
			reference := entry.Reference()
			mutate(&reference)
			if _, err := journal.ResolveReference(t.Context(), reference); err == nil {
				t.Fatal("invalid or mismatched role identity accepted")
			}
		})
	}
	for _, value := range []any{nil, "not JSON metadata", map[string]any{"unknown": true}, func() {},
		map[string]any{"resource_id": string(make([]byte, maxRecordBytes))}} {
		if _, err := ParseReference(value); err == nil {
			t.Fatalf("malformed reference accepted: %T", value)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := journal.ResolveReference(ctx, entry.Reference()); err == nil {
		t.Fatal("canceled reference read accepted")
	}
}
