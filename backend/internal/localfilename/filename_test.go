package localfilename

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func TestSafeMapsPortableFilenamesDeterministically(t *testing.T) {
	tests := map[string]string{
		"../report.txt":        "report.txt",
		"CON.txt":              "_CON.txt",
		"CONIN$":               "_CONIN$",
		"...":                  "___",
		"invoice":              "invoice",
		"invoice ":             "invoice_",
		"report.":              "report_",
		"bad\nname":            "badname",
		"invoice\u202ecod.exe": "invoice_cod.exe",
		"report\u2066.txt":     "report_.txt",
		"   ":                  "fallback.bin",
	}
	for input, want := range tests {
		if got := Safe(input, "fallback.bin"); got != want {
			t.Fatalf("Safe(%q) = %q, want %q", input, got, want)
		}
	}
	if got := Safe(strings.Repeat("x", MaxRunes+20), "fallback.bin"); len([]rune(got)) != MaxRunes {
		t.Fatalf("bounded filename length = %d", len([]rune(got)))
	}
	for _, suffix := range []string{".x", " x"} {
		got := Safe(strings.Repeat("a", MaxRunes-1)+suffix, "fallback.bin")
		if len([]rune(got)) != MaxRunes || strings.HasSuffix(got, ".") || strings.HasSuffix(got, " ") {
			t.Fatalf("post-truncation portable filename = %q", got)
		}
	}
}

func TestSafeFitsUTF8AndUTF16ComponentLimits(t *testing.T) {
	for _, input := range []string{
		strings.Repeat("界", MaxRunes),
		strings.Repeat("🙂", MaxRunes),
	} {
		got := Safe(input, "fallback.bin")
		if len([]rune(got)) > MaxRunes || len([]byte(got)) > MaxUTF8Bytes || len(utf16.Encode([]rune(got))) > MaxUTF16Units {
			t.Fatalf("filename exceeds portable limits: runes=%d utf8=%d utf16=%d", len([]rune(got)), len([]byte(got)), len(utf16.Encode([]rune(got))))
		}
	}
}

func TestFitWithSuffixBudgetsEveryPortableComponentLimit(t *testing.T) {
	suffix := "-2.json"
	for _, input := range []string{
		strings.Repeat("界", MaxRunes),
		strings.Repeat("🙂", MaxRunes),
	} {
		got := FitWithSuffix(input, suffix) + suffix
		if len([]rune(got)) > MaxRunes || len([]byte(got)) > MaxUTF8Bytes || len(utf16.Encode([]rune(got))) > MaxUTF16Units {
			t.Fatalf("suffixed filename exceeds portable limits: runes=%d utf8=%d utf16=%d", len([]rune(got)), len([]byte(got)), len(utf16.Encode([]rune(got))))
		}
	}
}
