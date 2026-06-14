package api

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

const approvalContextSchemaVersion = "connector-action-v1"

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func expired(value string, now time.Time) bool {
	if value == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return false
	}
	return !parsed.After(now)
}
