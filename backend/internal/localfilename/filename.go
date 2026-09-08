// Package localfilename maps remote object names to bounded, portable local filenames.
package localfilename

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	MaxRunes      = 160
	MaxUTF8Bytes  = 255
	MaxUTF16Units = 255
)

var windowsReservedNames = map[string]bool{
	"aux": true, "con": true, "conin$": true, "conout$": true, "nul": true, "prn": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// Safe returns one deterministic basename that can be saved on supported local filesystems.
func Safe(value, fallback string) string {
	if result := sanitize(value); result != "" {
		return result
	}
	if result := sanitize(fallback); result != "" {
		return result
	}
	return "aipermission-file"
}

func sanitize(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	value = path.Base(value)
	if value == "" || value == "/" || value == "." || value == ".." || strings.TrimSpace(value) == "" {
		return ""
	}
	var builder strings.Builder
	for _, character := range value {
		switch {
		case unicode.IsControl(character):
			continue
		case unicode.In(character, unicode.Cf):
			builder.WriteRune('_')
		case strings.ContainsRune(`<>:"/\\|?*`, character):
			builder.WriteRune('_')
		default:
			builder.WriteRune(character)
		}
	}
	value = portableEnding(builder.String())
	value = reserveWindowsDeviceName(value)
	return portableEnding(FitWithSuffix(value, ""))
}

func portableEnding(value string) string {
	trailing := len(value) - len(strings.TrimRight(value, " ."))
	if trailing > 0 {
		value = strings.TrimRight(value, " .") + strings.Repeat("_", trailing)
	}
	return value
}

func reserveWindowsDeviceName(value string) string {
	base := strings.TrimRight(strings.SplitN(value, ".", 2)[0], " .")
	if windowsReservedNames[strings.ToLower(base)] {
		return "_" + value
	}
	return value
}

// FitWithSuffix returns the longest prefix whose suffix-appended result fits
// the supported local filesystem component limits.
func FitWithSuffix(value, suffix string) string {
	remainingRunes := MaxRunes - len([]rune(suffix))
	remainingBytes := MaxUTF8Bytes - len(suffix)
	remainingUTF16 := MaxUTF16Units - len(utf16.Encode([]rune(suffix)))
	if remainingRunes < 0 || remainingBytes < 0 || remainingUTF16 < 0 {
		return ""
	}
	var builder strings.Builder
	runesUsed := 0
	bytesUsed := 0
	utf16Used := 0
	for _, character := range value {
		characterBytes := utf8.RuneLen(character)
		if characterBytes < 0 {
			characterBytes = utf8.RuneLen(utf8.RuneError)
		}
		characterUTF16 := 1
		if character > 0xffff {
			characterUTF16 = 2
		}
		if runesUsed+1 > remainingRunes || bytesUsed+characterBytes > remainingBytes || utf16Used+characterUTF16 > remainingUTF16 {
			break
		}
		builder.WriteRune(character)
		runesUsed++
		bytesUsed += characterBytes
		utf16Used += characterUTF16
	}
	return builder.String()
}
