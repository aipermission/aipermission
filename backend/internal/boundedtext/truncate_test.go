package boundedtext

import (
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8KeepsSuffixInsideByteBudget(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		limit  int
		suffix string
		want   string
	}{
		{name: "unchanged ascii", value: "hello", limit: 5, suffix: "...", want: "hello"},
		{name: "ascii suffix", value: "abcdef", limit: 5, suffix: "...", want: "ab..."},
		{name: "multibyte boundary", value: "ağşbc", limit: 6, suffix: ".", want: "ağş."},
		{name: "four byte boundary", value: "a😀b", limit: 5, suffix: ".", want: "a."},
		{name: "suffix larger than limit", value: "abcdef", limit: 2, suffix: "…x", want: ""},
		{name: "invalid input", value: string([]byte{'a', 0xff, 'b'}), limit: 4, suffix: ".", want: "a."},
		{name: "zero limit", value: "value", limit: 0, suffix: "...", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := TruncateUTF8(test.value, test.limit, test.suffix)
			if got != test.want {
				t.Fatalf("TruncateUTF8() = %q, want %q", got, test.want)
			}
			if !utf8.ValidString(got) || len(got) > test.limit {
				t.Fatalf("result is not valid and bounded: %q (%d > %d)", got, len(got), test.limit)
			}
		})
	}
}

func FuzzTruncateUTF8IsValidAndBounded(f *testing.F) {
	f.Add("hello", 5, "...")
	f.Add("😀😀", 7, "…")
	f.Add(string([]byte{0xff, 'x'}), 3, ".")
	f.Fuzz(func(t *testing.T, value string, limit int, suffix string) {
		if limit < 0 || limit > 4096 {
			return
		}
		result := TruncateUTF8(value, limit, suffix)
		if !utf8.ValidString(result) {
			t.Fatalf("invalid UTF-8 result: %q", result)
		}
		if len(result) > limit {
			t.Fatalf("result exceeded limit: %d > %d", len(result), limit)
		}
	})
}
