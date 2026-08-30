package api

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/aipermission/aipermission/backend/internal/expirypolicy"
)

const approvalContextSchemaVersion = "connector-action-v2"

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func expired(value string, now time.Time) bool {
	return !expirypolicy.Active(value, now)
}
