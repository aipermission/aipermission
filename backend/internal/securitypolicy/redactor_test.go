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

func TestRedactBasicPreservesExistingMandatoryMarkers(t *testing.T) {
	for _, marker := range []string{"[REDACTED]", "[REDACTED CREDENTIAL]", "[REDACTED VAULT VALUE]", "[REDACTED PRIVATE KEY]"} {
		for _, prefix := range []string{"password=", "token: ", "api-key='"} {
			closingQuote := ""
			if strings.HasSuffix(prefix, "'") {
				closingQuote = "'"
			}
			input := prefix + marker + closingQuote + " password=new-secret"
			want := prefix + marker + closingQuote + " password=[REDACTED]"
			if got := RedactBasic(input); got != want {
				t.Fatalf("masked text = %q; want %q", got, want)
			}
			if got := RedactBasic(want); got != want {
				t.Fatalf("repeated masked text = %q; want %q", got, want)
			}
		}
	}
}

func TestRedactBasicMasksQuotedMarkerDelimiterSuffix(t *testing.T) {
	for _, marker := range []string{"[REDACTED]", "[REDACTED CREDENTIAL]", "[REDACTED VAULT VALUE]", "[REDACTED PRIVATE KEY]"} {
		for _, quote := range []string{"'", `"`} {
			for _, delimiter := range []string{";", ","} {
				input := "password=" + quote + marker + delimiter + "synthetic-secret-7291" + quote
				want := "password=" + quote + "[REDACTED]" + quote
				if got := RedactBasic(input); got != want {
					t.Fatalf("masked quoted secret = %q; want %q", got, want)
				}
			}
		}
	}
}

func TestRedactBasicQuotedValuesAndIncompleteQuotes(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{`password="space separated secret"`, `password="[REDACTED]"`},
		{`password='embedded\'quote secret'`, `password='[REDACTED]'`},
		{`password="embedded\"quote secret"`, `password="[REDACTED]"`},
		{`password="unfinished-secret`, `password="[REDACTED]`},
		{`password='unfinished-secret`, `password='[REDACTED]`},
		{`PWD='/home/developer'`, `PWD='/home/developer'`},
	} {
		if got := RedactBasic(test.input); got != test.want {
			t.Fatalf("masked quoted value = %q; want %q", got, test.want)
		}
	}
}

func TestRedactBasicMasksMarkerPrefixedSecrets(t *testing.T) {
	for _, marker := range []string{"[REDACTED]", "[REDACTED CREDENTIAL]", "[REDACTED VAULT VALUE]", "[REDACTED PRIVATE KEY]"} {
		for _, quote := range []string{"", "'", `"`} {
			input := "password=" + quote + marker + "synthetic-secret-7291" + quote
			want := "password=" + quote + "[REDACTED]" + quote
			if got := RedactBasic(input); got != want {
				t.Fatalf("masked marker-prefixed secret = %q; want %q", got, want)
			}
		}
	}
}

func TestRedactBasicPreservesDelimitedMarkers(t *testing.T) {
	for _, marker := range []string{"[REDACTED]", "[REDACTED CREDENTIAL]", "[REDACTED VAULT VALUE]", "[REDACTED PRIVATE KEY]"} {
		for _, delimiter := range []string{";", ","} {
			input := "password=" + marker + delimiter + " unrelated=value"
			if got := RedactBasic(input); got != input {
				t.Fatalf("masked delimited marker = %q; want %q", got, input)
			}
		}
	}
}
