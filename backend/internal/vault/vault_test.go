package vault

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
)

type vaultSecret struct {
	Value string `json:"value"`
}

func TestVaultEncryptDecryptRoundTrip(t *testing.T) {
	first, err := New("secret-one")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	second, err := New("secret-one")
	if err != nil {
		t.Fatalf("new second vault: %v", err)
	}

	encrypted, err := first.EncryptJSON(vaultSecret{Value: "private"})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if encrypted == "" || encrypted == `{"value":"private"}` {
		t.Fatalf("secret was not encrypted: %q", encrypted)
	}

	var decoded vaultSecret
	if err := second.DecryptJSON(encrypted, &decoded); err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decoded.Value != "private" {
		t.Fatalf("unexpected value: %q", decoded.Value)
	}
}

func TestVaultRejectsWrongSecretAndMalformedPayloads(t *testing.T) {
	v, err := New("secret-one")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	encrypted, err := v.EncryptJSON(vaultSecret{Value: "private"})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	wrong, err := New("secret-two")
	if err != nil {
		t.Fatalf("new wrong vault: %v", err)
	}
	var decoded vaultSecret
	if err := wrong.DecryptJSON(encrypted, &decoded); err == nil {
		t.Fatalf("expected wrong secret to fail")
	}
	if err := v.DecryptJSON("not-base64", &decoded); err == nil {
		t.Fatalf("expected invalid base64 to fail")
	}
	if err := v.DecryptJSON("c2hvcnQ=", &decoded); err == nil {
		t.Fatalf("expected short payload to fail")
	}
}

func TestVaultUsesHKDFDerivedKeyMaterial(t *testing.T) {
	key, err := deriveVaultKey("secret-one")
	if err != nil {
		t.Fatalf("derive test key: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key))
	}
	rawSHA := sha256.Sum256([]byte("secret-one"))
	if bytes.Equal(key, rawSHA[:]) {
		t.Fatalf("vault key should not be the raw SHA-256 secret digest")
	}
}

func TestVaultEncryptDecryptWithAssociatedData(t *testing.T) {
	v, err := New("secret-one")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	aad := []byte{0, 1, 2, 3, 4}
	encrypted, err := v.EncryptJSONWithAAD(vaultSecret{Value: "private"}, aad)
	if err != nil {
		t.Fatalf("encrypt with aad: %v", err)
	}

	var decoded vaultSecret
	if err := v.DecryptJSONWithAAD(encrypted, &decoded, aad); err != nil {
		t.Fatalf("decrypt with aad: %v", err)
	}
	if decoded.Value != "private" {
		t.Fatalf("unexpected value: %q", decoded.Value)
	}
}

func TestVaultAssociatedDataFailsClosed(t *testing.T) {
	v, err := New("secret-one")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	encrypted, err := v.EncryptJSONWithAAD(vaultSecret{Value: "private"}, []byte("record-one"))
	if err != nil {
		t.Fatalf("encrypt with aad: %v", err)
	}

	var decoded vaultSecret
	if err := v.DecryptJSONWithAAD(encrypted, &decoded, []byte("record-two")); err == nil {
		t.Fatalf("expected mismatched aad to fail")
	}
	if err := v.DecryptJSON(encrypted, &decoded); err == nil {
		t.Fatalf("expected aad ciphertext to fail without aad")
	}
	if _, err := v.EncryptJSONWithAAD(vaultSecret{Value: "private"}, nil); err == nil {
		t.Fatalf("expected empty aad to fail encryption")
	}
	if err := v.DecryptJSONWithAAD(encrypted, &decoded, nil); err == nil {
		t.Fatalf("expected empty aad to fail decryption")
	}
}

func TestVaultRecordEnvelopeRoundTrip(t *testing.T) {
	v, err := New("secret-one")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	context := RecordContext{
		WorkspaceID: "workspace-1",
		Domain:      "connector-profile",
		RecordID:    "42",
		Field:       "encrypted_secret_json",
	}
	encrypted, err := v.EncryptRecordJSON(vaultSecret{Value: "private"}, context)
	if err != nil {
		t.Fatalf("encrypt record: %v", err)
	}
	if !IsRecordEnvelope(encrypted) || strings.Contains(encrypted, "private") {
		t.Fatalf("unexpected encrypted record envelope: %q", encrypted)
	}

	var envelope recordEnvelope
	if err := json.Unmarshal([]byte(encrypted), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Version != recordEnvelopeVersion || envelope.Algorithm != recordEnvelopeAlgorithm {
		t.Fatalf("unexpected envelope metadata: %+v", envelope)
	}

	var decoded vaultSecret
	if err := v.DecryptRecordJSON(encrypted, &decoded, context); err != nil {
		t.Fatalf("decrypt record: %v", err)
	}
	if decoded.Value != "private" {
		t.Fatalf("unexpected value: %q", decoded.Value)
	}
}

func TestVaultRecordEnvelopeRejectsContextSwap(t *testing.T) {
	v, err := New("secret-one")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	context := RecordContext{WorkspaceID: "workspace-1", Domain: "token", RecordID: "7", Field: "token_value"}
	encrypted, err := v.EncryptRecordJSON(vaultSecret{Value: "private"}, context)
	if err != nil {
		t.Fatalf("encrypt record: %v", err)
	}

	mutations := []RecordContext{
		{WorkspaceID: "workspace-2", Domain: context.Domain, RecordID: context.RecordID, Field: context.Field},
		{WorkspaceID: context.WorkspaceID, Domain: "ssh-key", RecordID: context.RecordID, Field: context.Field},
		{WorkspaceID: context.WorkspaceID, Domain: context.Domain, RecordID: "8", Field: context.Field},
		{WorkspaceID: context.WorkspaceID, Domain: context.Domain, RecordID: context.RecordID, Field: "other_field"},
	}
	for _, mutated := range mutations {
		var decoded vaultSecret
		if err := v.DecryptRecordJSON(encrypted, &decoded, mutated); err == nil {
			t.Fatalf("expected context swap to fail: %+v", mutated)
		}
	}
}

func TestVaultRecordEnvelopeRejectsMalformedAndUnknownVersions(t *testing.T) {
	v, err := New("secret-one")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	context := RecordContext{WorkspaceID: "workspace-1", Domain: "token", RecordID: "7", Field: "token_value"}
	var decoded vaultSecret
	for _, encrypted := range []string{
		`{"version":2,"algorithm":"AES-256-GCM","nonce":"AA==","ciphertext":"AA=="}`,
		`{"version":1,"algorithm":"other","nonce":"AA==","ciphertext":"AA=="}`,
		`{"version":1`,
	} {
		if err := v.DecryptRecordJSON(encrypted, &decoded, context); err == nil {
			t.Fatalf("expected invalid envelope to fail: %s", encrypted)
		}
	}
	if _, err := v.EncryptRecordJSON(vaultSecret{Value: "private"}, RecordContext{}); err == nil {
		t.Fatalf("expected empty record context to fail")
	}
}

func TestVaultRecordEnvelopeLegacyBridgeIsExplicit(t *testing.T) {
	v, err := New("secret-one")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	context := RecordContext{WorkspaceID: "workspace-1", Domain: "token", RecordID: "7", Field: "token_value"}
	legacy, err := v.EncryptJSON(vaultSecret{Value: "private"})
	if err != nil {
		t.Fatalf("encrypt legacy record: %v", err)
	}

	var decoded vaultSecret
	if err := v.DecryptRecordJSON(legacy, &decoded, context); err == nil {
		t.Fatalf("strict record decryption must reject a legacy payload")
	}
	wasLegacy, err := v.DecryptRecordJSONWithLegacy(legacy, &decoded, context, nil)
	if err != nil {
		t.Fatalf("decrypt legacy record: %v", err)
	}
	if !wasLegacy || decoded.Value != "private" {
		t.Fatalf("unexpected legacy bridge result: legacy=%t value=%q", wasLegacy, decoded.Value)
	}

	versioned, err := v.EncryptRecordJSON(vaultSecret{Value: "private"}, context)
	if err != nil {
		t.Fatalf("encrypt versioned record: %v", err)
	}
	wasLegacy, err = v.DecryptRecordJSONWithLegacy(versioned, &decoded, context, nil)
	if err != nil {
		t.Fatalf("decrypt versioned record: %v", err)
	}
	if wasLegacy {
		t.Fatalf("versioned record was reported as legacy")
	}
}
