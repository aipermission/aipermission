package connectors

import (
	"os/exec"
	"runtime"
	"testing"
)

func TestQuoteShellArgumentEncoding(t *testing.T) {
	for _, test := range []struct{ value, want string }{
		{"", "''"},
		{"two words", "'two words'"},
		{"a'b", "'a'\"'\"'b'"},
		{"$HOME; `printf bad`", "'$HOME; `printf bad`'"},
	} {
		if got := QuoteShellArgument(test.value); got != test.want {
			t.Fatalf("Quote(%q)=%q, want %q", test.value, got, test.want)
		}
	}
}

func TestQuoteShellArgumentRoundTripsThroughPOSIXShellWithoutExpansion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell execution is not available on native Windows")
	}
	for _, value := range []string{"", "two words", "a'b", "\"quoted\"", "line\nnext", "; printf injected", "$(printf expanded)", "`printf expanded`", "$HOME", "\\path\\", "caf\u00e9"} {
		t.Run(value, func(t *testing.T) {
			output, err := exec.CommandContext(t.Context(), "/bin/sh", "-c", "printf '%s' "+QuoteShellArgument(value)).CombinedOutput()
			if err != nil || string(output) != value {
				t.Fatalf("output=%q error=%v, want %q", output, err, value)
			}
		})
	}
}
