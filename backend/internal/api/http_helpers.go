package api

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/tokens"
)

const maxAttachmentFilenameRunes = 180

type httpDomainError struct {
	NotFound        error
	NotFoundMessage string
	FailureMessage  string
	Validation      func(error) (string, bool)
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func (s *Server) activeRuntimeOrLocked(w http.ResponseWriter) (*databaseRuntime, bool) {
	runtime := s.activeRuntime()
	if runtime == nil {
		writeError(w, http.StatusLocked, "database is locked")
		return nil, false
	}
	return runtime, true
}

func parseInt64Query(w http.ResponseWriter, value string, name string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return id, true
}

func handleTokenError(w http.ResponseWriter, err error) {
	handleDomainError(w, err, httpDomainError{
		NotFound:        tokens.ErrNotFound,
		NotFoundMessage: "token not found",
		FailureMessage:  "token operation failed",
		Validation: func(err error) (string, bool) {
			var validation tokens.ValidationError
			if errors.As(err, &validation) {
				return validation.Error(), true
			}
			return "", false
		},
	})
}

func handleDomainError(w http.ResponseWriter, err error, domain httpDomainError) {
	if errors.Is(err, domain.NotFound) {
		writeError(w, http.StatusNotFound, domain.NotFoundMessage)
		return
	}
	if domain.Validation != nil {
		if message, ok := domain.Validation(err); ok {
			writeError(w, http.StatusBadRequest, message)
			return
		}
	}
	if domain.FailureMessage != "" {
		writeError(w, http.StatusInternalServerError, domain.FailureMessage)
		return
	}
	writeInternalError(w)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeSensitiveJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, status, payload)
}

func setAttachmentHeaders(w http.ResponseWriter, filename, contentType string) {
	filename = safeAttachmentFilename(filename, "aipermission-download")
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
}

func safeAttachmentFilename(value, fallback string) string {
	if value = normalizeAttachmentFilename(value); value != "" {
		return value
	}
	if fallback = normalizeAttachmentFilename(fallback); fallback != "" {
		return fallback
	}
	return "aipermission-download"
}

func normalizeAttachmentFilename(value string) string {
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
	if len(runes) > maxAttachmentFilenameRunes {
		value = string(runes[:maxAttachmentFilenameRunes])
	}
	return value
}
