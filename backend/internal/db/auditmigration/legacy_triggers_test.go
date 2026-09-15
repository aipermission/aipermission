package auditmigration

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestLegacyTriggersPreserveVersion13Payload(t *testing.T) {
	statements := LegacyTriggers()
	if len(statements) != 9 {
		t.Fatalf("version-13 audit trigger count = %d, want 9", len(statements))
	}
	// This fingerprint comes from the original version-13 migration, before extraction.
	digest := sha256.Sum256([]byte(strings.Join(statements, "\x00")))
	const originalDigest = "1eb031e34930d413f6d20e7f8ad0517a66c72937a666195d97f20cadababf1eb"
	if got := hex.EncodeToString(digest[:]); got != originalDigest {
		t.Fatalf("applied version-13 audit SQL changed: SHA-256 = %s", got)
	}
}
