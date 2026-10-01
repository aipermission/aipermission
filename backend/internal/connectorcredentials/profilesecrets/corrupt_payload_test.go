package profilesecrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

func TestProfileSecretCodecDiscardsPartiallyDecodedAuthenticatedPayload(t *testing.T) {
	secretVault, codec := newProfileCodecFixture(t)
	encrypted := authenticatedCorruptProfileFixture(t)
	partial := map[string]any{}
	if err := recordcrypto.DecryptJSON(secretVault, "fixture-workspace", recordcrypto.ConnectorCredentialProfile, 7, encrypted, &partial); err == nil || partial["password"] != "fixture-secret" {
		t.Fatalf("fixture did not reach partial JSON decoding: %#v %v", partial, err)
	}
	if secret, err := codec.Decrypt(t.Context(), 7, encrypted); err == nil || secret != nil {
		t.Fatalf("partially decoded secret escaped error boundary: %#v %v", secret, err)
	}
}

// Build a deliberately invalid authenticated record with the known fixture
// key. Keeping this test-only avoids granting production code a raw seal port.
func authenticatedCorruptProfileFixture(t *testing.T) string {
	t.Helper()
	key, err := hkdf.Key(sha256.New, []byte("ProfileCodecFixturePassword123"), []byte("aipermission-vault-salt-v1"), "aipermission gateway vault aes-gcm key v1", 32)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	aad := []byte(`{"version":"aipermission-record-aad-v1","workspace_id":"fixture-workspace","domain":"connector-credential-profile","record_id":"7","field":"encrypted_secret_json"}`)
	envelope := struct {
		Version    int    `json:"version"`
		Algorithm  string `json:"algorithm"`
		Nonce      string `json:"nonce"`
		Ciphertext string `json:"ciphertext"`
	}{1, "AES-256-GCM", base64.StdEncoding.EncodeToString(nonce), base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, []byte(`{"password":"fixture-secret"}{}`), aad))}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
