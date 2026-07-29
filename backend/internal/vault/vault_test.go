package vault

import (
	"bytes"
	"crypto/sha256"
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
