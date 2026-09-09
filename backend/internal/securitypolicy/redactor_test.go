package securitypolicy

import (
	"strings"
	"testing"
)

func TestRedactBasicMasksCommonSecretShapes(t *testing.T) {
	input := strings.Join([]string{
		"password=super-secret",
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456",
		"token: ghp_abcdefghijklmnopqrstuvwxyz123456",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----",
	}, "\n")
	output := RedactBasic(input)
	for _, secret := range []string{"super-secret", "abcdefghijklmnopqrstuvwxyz123456", "abc\n-----END"} {
		if strings.Contains(output, secret) {
			t.Fatalf("secret fragment %q was not redacted: %s", secret, output)
		}
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Fatalf("expected redaction marker: %s", output)
	}
}

func TestRedactBasicKeepsShellPWDOutput(t *testing.T) {
	output := RedactBasic("PWD=/home/developer/workspace\npwd=super-secret\nPASSWORD=another-secret")
	if !strings.Contains(output, "PWD=/home/developer/workspace") {
		t.Fatalf("shell PWD output should not be redacted: %s", output)
	}
	for _, secret := range []string{"super-secret", "another-secret"} {
		if strings.Contains(output, secret) {
			t.Fatalf("secret %q was not redacted: %s", secret, output)
		}
	}
}
