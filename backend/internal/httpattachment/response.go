// Package httpattachment owns safe local download response headers.
package httpattachment

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

const MaxFilenameRunes = 180

func SetHeaders(w http.ResponseWriter, filename, contentType string) {
	filename = SafeFilename(filename, "aipermission-download")
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
}

func SafeFilename(value, fallback string) string {
	if value = normalizeFilename(value); value != "" {
		return value
	}
	if fallback = normalizeFilename(fallback); fallback != "" {
		return fallback
	}
	return "aipermission-download"
}

func normalizeFilename(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	value = filepath.Base(strings.TrimSpace(value))
	value = strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return -1
		}
		return character
	}, value)
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == "/" {
		return ""
	}
	runes := []rune(value)
	if len(runes) > MaxFilenameRunes {
		value = string(runes[:MaxFilenameRunes])
	}
	return value
}
