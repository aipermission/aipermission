package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	recordEnvelopeVersion   = 1
	recordEnvelopeAlgorithm = "AES-256-GCM"
	recordAADVersion        = "aipermission-record-aad-v1"
)

type RecordContext struct {
	WorkspaceID string
	Domain      string
	RecordID    string
	Field       string
}

type recordEnvelope struct {
	Version    int    `json:"version"`
	Algorithm  string `json:"algorithm"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type recordAAD struct {
	Version     string `json:"version"`
	WorkspaceID string `json:"workspace_id"`
	Domain      string `json:"domain"`
	RecordID    string `json:"record_id"`
	Field       string `json:"field"`
}

type Vault struct {
	aead cipher.AEAD
}

var (
	// HKDF salt is public domain-separation data, not a secret. Security comes
	// from the gateway secret entropy; changing this value would break stored
	// vault payloads for no security gain.
	vaultHKDFSalt = []byte("aipermission-vault-salt-v1")
	vaultHKDFInfo = "aipermission gateway vault aes-gcm key v1"
)

func New(secret string) (*Vault, error) {
	key, err := deriveVaultKey(secret)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return &Vault{aead: aead}, nil
}

func deriveVaultKey(secret string) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, []byte(secret), vaultHKDFSalt, vaultHKDFInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("derive vault key: %w", err)
	}
	return key, nil
}

func (v *Vault) EncryptJSON(value any) (string, error) {
	return v.encryptJSON(value, nil)
}

func (v *Vault) EncryptJSONWithAAD(value any, associatedData []byte) (string, error) {
	if len(associatedData) == 0 {
		return "", fmt.Errorf("associated data is required")
	}
	return v.encryptJSON(value, associatedData)
}

// EncryptRecordJSON creates a versioned envelope bound to one immutable
// workspace/domain/record/field identity. Moving the stored value to another
// record or encrypted field makes authentication fail.
func (v *Vault) EncryptRecordJSON(value any, context RecordContext) (string, error) {
	associatedData, err := recordAssociatedData(context)
	if err != nil {
		return "", err
	}
	plain, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal secret: %w", err)
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("create nonce: %w", err)
	}
	ciphertext := v.aead.Seal(nil, nonce, plain, associatedData)
	envelope, err := json.Marshal(recordEnvelope{
		Version:    recordEnvelopeVersion,
		Algorithm:  recordEnvelopeAlgorithm,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	})
	if err != nil {
		return "", fmt.Errorf("marshal encrypted record envelope: %w", err)
	}
	return string(envelope), nil
}

// DecryptRecordJSON accepts only the current versioned record envelope.
func (v *Vault) DecryptRecordJSON(encrypted string, target any, context RecordContext) error {
	return v.decryptRecordJSON(encrypted, target, context)
}

// DecryptRecordJSONWithLegacy is an explicit migration bridge. A versioned
// envelope is never retried as legacy after authentication or parsing fails.
// legacyAssociatedData may be nil for records encrypted without AAD.
func (v *Vault) DecryptRecordJSONWithLegacy(encrypted string, target any, context RecordContext, legacyAssociatedData []byte) (bool, error) {
	if IsRecordEnvelope(encrypted) {
		return false, v.decryptRecordJSON(encrypted, target, context)
	}
	if err := v.decryptJSON(encrypted, target, legacyAssociatedData); err != nil {
		return true, err
	}
	return true, nil
}

func IsRecordEnvelope(encrypted string) bool {
	return strings.HasPrefix(strings.TrimSpace(encrypted), "{")
}

func (v *Vault) decryptRecordJSON(encrypted string, target any, context RecordContext) error {
	associatedData, err := recordAssociatedData(context)
	if err != nil {
		return err
	}
	var envelope recordEnvelope
	if err := json.Unmarshal([]byte(encrypted), &envelope); err != nil {
		return fmt.Errorf("decode encrypted record envelope: %w", err)
	}
	if envelope.Version != recordEnvelopeVersion {
		return fmt.Errorf("unsupported encrypted record envelope version %d", envelope.Version)
	}
	if envelope.Algorithm != recordEnvelopeAlgorithm {
		return fmt.Errorf("unsupported encrypted record algorithm %q", envelope.Algorithm)
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return fmt.Errorf("decode encrypted record nonce: %w", err)
	}
	if len(nonce) != v.aead.NonceSize() {
		return fmt.Errorf("encrypted record nonce has invalid length")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return fmt.Errorf("decode encrypted record ciphertext: %w", err)
	}
	plain, err := v.aead.Open(nil, nonce, ciphertext, associatedData)
	if err != nil {
		return fmt.Errorf("decrypt encrypted record: %w", err)
	}
	if err := json.Unmarshal(plain, target); err != nil {
		return fmt.Errorf("unmarshal secret: %w", err)
	}
	return nil
}

func recordAssociatedData(context RecordContext) ([]byte, error) {
	context.WorkspaceID = strings.TrimSpace(context.WorkspaceID)
	context.Domain = strings.TrimSpace(context.Domain)
	context.RecordID = strings.TrimSpace(context.RecordID)
	context.Field = strings.TrimSpace(context.Field)
	if context.WorkspaceID == "" || context.Domain == "" || context.RecordID == "" || context.Field == "" {
		return nil, fmt.Errorf("record encryption context requires workspace_id, domain, record_id, and field")
	}
	encoded, err := json.Marshal(recordAAD{
		Version:     recordAADVersion,
		WorkspaceID: context.WorkspaceID,
		Domain:      context.Domain,
		RecordID:    context.RecordID,
		Field:       context.Field,
	})
	if err != nil {
		return nil, fmt.Errorf("encode record encryption context: %w", err)
	}
	return encoded, nil
}

func (v *Vault) encryptJSON(value any, associatedData []byte) (string, error) {
	plain, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal secret: %w", err)
	}

	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("create nonce: %w", err)
	}

	ciphertext := v.aead.Seal(nil, nonce, plain, associatedData)
	payload := append(nonce, ciphertext...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func (v *Vault) DecryptJSON(encrypted string, target any) error {
	return v.decryptJSON(encrypted, target, nil)
}

func (v *Vault) DecryptJSONWithAAD(encrypted string, target any, associatedData []byte) error {
	if len(associatedData) == 0 {
		return fmt.Errorf("associated data is required")
	}
	return v.decryptJSON(encrypted, target, associatedData)
}

func (v *Vault) decryptJSON(encrypted string, target any, associatedData []byte) error {
	payload, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return fmt.Errorf("decode secret: %w", err)
	}

	nonceSize := v.aead.NonceSize()
	if len(payload) < nonceSize {
		return fmt.Errorf("secret payload is too short")
	}

	nonce := payload[:nonceSize]
	ciphertext := payload[nonceSize:]
	plain, err := v.aead.Open(nil, nonce, ciphertext, associatedData)
	if err != nil {
		return fmt.Errorf("decrypt secret: %w", err)
	}

	if err := json.Unmarshal(plain, target); err != nil {
		return fmt.Errorf("unmarshal secret: %w", err)
	}
	return nil
}
