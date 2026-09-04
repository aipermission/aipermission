package api

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const connectorActionIdentityVersion = "h1:"

func deriveConnectorActionIdentityKey(gatewaySecret, workspaceID string) ([]byte, error) {
	if gatewaySecret == "" || strings.TrimSpace(workspaceID) == "" {
		return nil, fmt.Errorf("connector action identity requires gateway secret and workspace identity")
	}
	key, err := hkdf.Key(
		sha256.New,
		[]byte(gatewaySecret),
		[]byte("aipermission-connector-action-identity-v1\x00"+strings.TrimSpace(workspaceID)),
		"idempotency identity authentication",
		32,
	)
	if err != nil {
		return nil, fmt.Errorf("derive connector action identity key: %w", err)
	}
	return key, nil
}

func connectorActionIdentityTag(key, canonical []byte) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("connector action identity key is unavailable")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("aipermission-connector-action-call-v1\x00"))
	_, _ = mac.Write(canonical)
	return connectorActionIdentityVersion + hex.EncodeToString(mac.Sum(nil)), nil
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
