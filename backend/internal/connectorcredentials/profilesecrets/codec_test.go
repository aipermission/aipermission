package profilesecrets

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func newProfileCodecFixture(t *testing.T) (*vault.Vault, ProfileSecretCodec) {
	t.Helper()
	secretVault, err := vault.New("ProfileCodecFixturePassword123")
	if err != nil {
		t.Fatal(err)
	}
	return secretVault, NewProfileSecretCodec(secretVault, "fixture-workspace")
}

func TestProfileSecretCodecPreservesExistingRecordEnvelope(t *testing.T) {
	secretVault, codec := newProfileCodecFixture(t)
	secret := map[string]any{"password": "fixture-secret", "count": json.Number("9007199254740993"), "nested": map[string]any{"enabled": true}}
	legacyCaller, err := recordcrypto.EncryptJSON(secretVault, "fixture-workspace", recordcrypto.ConnectorCredentialProfile, 7, secret)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decrypt(t.Context(), 7, legacyCaller)
	if err != nil || !reflect.DeepEqual(decoded, secret) {
		t.Fatalf("existing envelope changed: %#v %v", decoded, err)
	}
	encoded, err := codec.Encrypt(t.Context(), 7, secret)
	if err != nil {
		t.Fatal(err)
	}
	var oldReader map[string]any
	if err := recordcrypto.DecryptJSON(secretVault, "fixture-workspace", recordcrypto.ConnectorCredentialProfile, 7, encoded, &oldReader); err != nil || !reflect.DeepEqual(oldReader, secret) {
		t.Fatalf("new envelope changed existing AAD contract: %#v %v", oldReader, err)
	}
	decoded["password"] = "changed"
	if next, err := codec.Decrypt(t.Context(), 7, encoded); err != nil || next["password"] != "fixture-secret" {
		t.Fatalf("codec cached a mutable decoded secret: %#v %v", next, err)
	}
	if secret["password"] != "fixture-secret" {
		t.Fatal("codec mutated input secrets")
	}
}

func TestProfileSecretCodecRejectsWrongWorkspaceProfileAndKey(t *testing.T) {
	_, codec := newProfileCodecFixture(t)
	wrongVault, err := vault.New("DifferentProfileCodecPassword123")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := codec.Encrypt(t.Context(), 7, map[string]any{"password": "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name  string
		codec ProfileSecretCodec
		id    int64
	}{
		{"workspace", NewProfileSecretCodec(codec.vault, "other-workspace"), 7},
		{"profile", codec, 8}, {"key", NewProfileSecretCodec(wrongVault, "fixture-workspace"), 7},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if got, err := scenario.codec.Decrypt(t.Context(), scenario.id, encrypted); err == nil || got != nil {
				t.Fatalf("wrong binding leaked secrets: %#v %v", got, err)
			}
		})
	}
}

func TestProfileSecretCodecRejectsOtherRecordClassesAndInvalidPayloads(t *testing.T) {
	secretVault, codec := newProfileCodecFixture(t)
	for _, recordType := range []recordcrypto.RecordType{recordcrypto.ConnectorCredentialResource, recordcrypto.APIToken} {
		encrypted, err := recordcrypto.EncryptJSON(secretVault, "fixture-workspace", recordType, 7, map[string]any{"password": "fixture-secret"})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := codec.Decrypt(t.Context(), 7, encrypted); err == nil || got != nil {
			t.Fatalf("foreign record accepted: %#v %v", got, err)
		}
	}
	for _, value := range []any{json.RawMessage(`{"password":"fixture-secret","nested":42}`), []string{"not", "a", "map"}} {
		encrypted, err := recordcrypto.EncryptJSON(secretVault, "fixture-workspace", recordcrypto.ConnectorCredentialProfile, 7, value)
		if err != nil {
			t.Fatal(err)
		}
		if _, array := value.([]string); array {
			if got, err := codec.Decrypt(t.Context(), 7, encrypted); err == nil || got != nil {
				t.Fatalf("invalid secret shape accepted: %#v %v", got, err)
			}
		} else if got, err := codec.Decrypt(t.Context(), 7, encrypted); err != nil || got["password"] != "fixture-secret" {
			t.Fatalf("valid raw JSON changed: %#v %v", got, err)
		}
	}
	for _, encrypted := range []string{"", "invalid", `{"version":99}`, `{"version":1,"algorithm":"unknown"}`} {
		if got, err := codec.Decrypt(t.Context(), 7, encrypted); err == nil || got != nil {
			t.Fatalf("malformed envelope leaked secrets: %#v %v", got, err)
		}
	}
}

func TestProfileSecretCodecEncryptionFailureReturnsNoEnvelope(t *testing.T) {
	_, codec := newProfileCodecFixture(t)
	if got, err := codec.Encrypt(t.Context(), 7, map[string]any{"invalid": func() {}}); err == nil || got != "" {
		t.Fatalf("unsupported secret value produced an envelope: %q %v", got, err)
	}
}

func TestProfileSecretCodecUnavailableStorageFailsWithoutPanic(t *testing.T) {
	secretVault, _ := newProfileCodecFixture(t)
	for _, codec := range []ProfileSecretCodec{{}, NewProfileSecretCodec(nil, "workspace"), NewProfileSecretCodec(secretVault, ""), NewProfileSecretCodec(secretVault, " \t ")} {
		if codec.Available() {
			t.Fatal("unavailable codec reported ready")
		}
		if got, err := codec.Encrypt(t.Context(), 7, map[string]any{}); err == nil || got != "" {
			t.Fatalf("unavailable encryption: %q %v", got, err)
		}
		if got, err := codec.Decrypt(t.Context(), 7, "invalid"); err == nil || got != nil {
			t.Fatalf("unavailable decryption: %#v %v", got, err)
		}
	}
}
