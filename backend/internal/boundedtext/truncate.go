// Package boundedtext provides byte-bounded, UTF-8-safe text helpers.
package boundedtext

import (
	"strings"
	"unicode/utf8"
)

// TruncateUTF8 returns valid UTF-8 whose encoded size does not exceed maxBytes.
// When truncation is required, suffix is included inside the byte budget.
func TruncateUTF8(value string, maxBytes int, suffix string) string {
	if maxBytes <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(value, "\uFFFD")
	if len(value) <= maxBytes {
		return value
	}
	suffix = strings.ToValidUTF8(suffix, "\uFFFD")
	if len(suffix) >= maxBytes {
		return prefixUTF8(suffix, maxBytes)
	}
	return prefixUTF8(value, maxBytes-len(suffix)) + suffix
}

func prefixUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}
