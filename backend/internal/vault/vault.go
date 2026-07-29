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
)

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
