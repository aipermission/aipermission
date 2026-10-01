package connectortargets

import "testing"

func TestPublishedProfileComparisonRequiresFullExactEvidence(t *testing.T) {
	expected := CredentialProfile{
		ID: 2, TargetID: 1, ConnectorKind: "test", Kind: "operator", Label: "managed",
		Public: map[string]any{"identity": "exact", "count": 3}, RiskLabel: "write",
		EncryptedSecretJSON: "encrypted", SecretRevision: 2, CreatedAt: "created", UpdatedAt: "updated",
	}
	for _, test := range []struct {
		name   string
		change func(*CredentialProfile)
	}{
		{"id", func(p *CredentialProfile) { p.ID++ }},
		{"target", func(p *CredentialProfile) { p.TargetID++ }},
		{"connector", func(p *CredentialProfile) { p.ConnectorKind = "other" }},
		{"kind", func(p *CredentialProfile) { p.Kind = "other" }},
		{"label", func(p *CredentialProfile) { p.Label += " " }},
		{"risk", func(p *CredentialProfile) { p.RiskLabel = "read" }},
		{"secret", func(p *CredentialProfile) { p.EncryptedSecretJSON = "other" }},
		{"empty secret", func(p *CredentialProfile) { p.EncryptedSecretJSON = "" }},
		{"revision", func(p *CredentialProfile) { p.SecretRevision++ }},
		{"created", func(p *CredentialProfile) { p.CreatedAt = "other" }},
		{"updated", func(p *CredentialProfile) { p.UpdatedAt = "other" }},
		{"public", func(p *CredentialProfile) { p.Public = map[string]any{"identity": "other"} }},
		{"unsupported public", func(p *CredentialProfile) { p.Public = map[string]any{"bad": make(chan int)} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual := expected
			test.change(&actual)
			if samePublishedProfile(actual, expected) {
				t.Fatal("changed publication evidence was accepted")
			}
		})
	}
	actual := expected
	actual.Public = map[string]any{"count": float64(3), "identity": "exact"}
	if !samePublishedProfile(actual, expected) {
		t.Fatal("canonical JSON-equivalent roundtrip evidence was rejected")
	}
	expected.Public = map[string]any{"bad": make(chan int)}
	if samePublishedProfile(actual, expected) {
		t.Fatal("unsupported expected metadata was accepted")
	}
}

func TestProfilePublicationUnavailableEvidenceFailsClosed(t *testing.T) {
	var store *Store
	for _, profile := range []CredentialProfile{{}, {ID: 1}, {ID: 1, TargetID: 1}} {
		if _, confirmed := store.ConfirmCredentialProfilePublication(t.Context(), profile); confirmed {
			t.Fatal("unavailable profile evidence was confirmed")
		}
	}
	if exists, err := store.HasCredentialProfileLabel(t.Context(), 1, "managed"); err == nil || exists {
		t.Fatal("unavailable label lookup was accepted")
	}
}
