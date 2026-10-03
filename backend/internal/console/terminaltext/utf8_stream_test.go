package terminaltext

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestUTF8StreamEveryPartitionAndBoundedCarry(t *testing.T) {
	for _, text := range []string{"", "ASCII", "a\u00f6\u20ac\U0001f680z\x1b[0m", strings.Repeat("a", 4095) + "\U0001f680"} {
		for size := 1; size <= utf8.UTFMax; size++ {
			var decoder UTF8Stream
			var output strings.Builder
			for start := 0; start < len(text); start += size {
				part := decoder.Write([]byte(text[start:min(start+size, len(text))]), false)
				if !utf8.ValidString(part) || decoder.length > utf8.UTFMax-1 {
					t.Fatal("invalid frame or unbounded carry")
				}
				output.WriteString(part)
			}
			output.WriteString(decoder.Write(nil, true))
			if output.String() != text || decoder.length != 0 {
				t.Fatal("text changed or final carry retained")
			}
		}
	}
}

func TestUTF8StreamInvalidAndUnfinishedBytesHaveStableEOFPolicy(t *testing.T) {
	for _, text := range []string{"\xff\xfe", "a\xe2\x82", "\xed\xa0\x80", "\xe2(\xa1", "\xf0\x9f\x9a"} {
		for size := 1; size <= len(text); size++ {
			var decoder UTF8Stream
			var output strings.Builder
			for start := 0; start < len(text); start += size {
				output.WriteString(decoder.Write([]byte(text[start:min(start+size, len(text))]), false))
			}
			output.WriteString(decoder.Write(nil, true))
			expected := string([]rune(text))
			if output.String() != expected || decoder.length != 0 || decoder.Write(nil, true) != "" {
				t.Fatalf("EOF policy changed: %q, expected %q", output.String(), expected)
			}
		}
	}
}
