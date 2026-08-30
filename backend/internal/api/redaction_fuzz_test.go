package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func FuzzBasicRedaction(f *testing.F) {
	privateKeyBegin := "-----BEGIN " + "PRIVATE KEY-----"
	privateKeyEnd := "-----END " + "PRIVATE KEY-----"
	f.Add([]byte("password=super-secret"))
	f.Add([]byte("Authorization: Bearer abc.def-123"))
	f.Add([]byte(privateKeyBegin + "\nsecret\n" + privateKeyEnd))
	f.Add([]byte("PWD=/home/developer/workspace"))

	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 128<<10 {
			return
		}
		digest := sha256.Sum256(input)
		secret := "fuzz_" + hex.EncodeToString(digest[:])
		commonToken := "ghp_" + hex.EncodeToString(digest[:])
		value := fmt.Sprintf(
			"%s\npassword=%s\nAuthorization: Bearer %s\n%s\n%s\n%s\n%s",
			string(input), secret, secret, commonToken, privateKeyBegin, secret, privateKeyEnd,
		)
		redacted := redactBasic(value)
		if strings.Contains(redacted, secret) || strings.Contains(redacted, commonToken) {
			t.Fatalf("synthetic secret survived redaction")
		}
		if repeated := redactBasic(redacted); repeated != redacted {
			t.Fatalf("basic redaction is not idempotent")
		}
		if len(redacted) > len(value)*4+1024 {
			t.Fatalf("redacted output expanded unexpectedly: input=%d output=%d", len(value), len(redacted))
		}
	})
}
