package connectortargets

import "testing"

func TestSQLCipherProfilePublicationRequiresFreshActiveExactRow(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	target, err := store.CreateTarget(t.Context(), CreateTargetInput{ConnectorKind: "fixture", Name: "Test target"})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: target.ConnectorKind, Kind: "operator", Label: " Managed ",
		Public: map[string]any{"identity": "exact"}, EncryptedSecretJSON: "encrypted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, confirmed := store.ConfirmCredentialProfilePublication(t.Context(), profile); !confirmed {
		t.Fatal("fresh committed profile could not be confirmed")
	}
	if exists, err := store.HasCredentialProfileLabel(t.Context(), target.ID, "managed"); err != nil || !exists {
		t.Fatalf("label normalization changed: %v %v", exists, err)
	}
	if exists, err := store.HasCredentialProfileLabel(t.Context(), target.ID, "other"); err != nil || exists {
		t.Fatalf("missing label was accepted: %v %v", exists, err)
	}
	changed := profile
	changed.Public = map[string]any{"identity": "changed"}
	if _, confirmed := store.ConfirmCredentialProfilePublication(t.Context(), changed); confirmed {
		t.Fatal("different public identity was accepted")
	}
	if err := store.DeleteCredentialProfile(t.Context(), target.ID, profile.ID); err != nil {
		t.Fatal(err)
	}
	if _, confirmed := store.ConfirmCredentialProfilePublication(t.Context(), profile); confirmed {
		t.Fatal("archived profile was accepted as a committed publication")
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, confirmed := store.ConfirmCredentialProfilePublication(t.Context(), profile); confirmed {
		t.Fatal("failed storage read was accepted as confirmation")
	}
	if exists, err := store.HasCredentialProfileLabel(t.Context(), target.ID, "managed"); err == nil || exists {
		t.Fatal("failed storage read was accepted as label absence")
	}
}
