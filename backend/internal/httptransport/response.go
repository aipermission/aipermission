// Package httptransport owns shared HTTP encoding and primitive parsing for
// the local gateway's domain-specific transport adapters.
package httptransport

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func WriteSensitiveJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	WriteJSON(w, status, payload)
}

func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, ErrorResponse{Error: message})
}

func WriteErrorCode(w http.ResponseWriter, status int, message, code string) {
	WriteJSON(w, status, ErrorResponse{Error: message, Code: code})
}

func WriteInternalError(w http.ResponseWriter) {
	WriteError(w, http.StatusInternalServerError, "internal server error")
}

func ParsePathInt64(w http.ResponseWriter, r *http.Request, key, label string) (int64, bool) {
	return ParsePositiveInt64(w, r.PathValue(key), label)
}

func ParseQueryInt64(w http.ResponseWriter, value, name string) (int64, bool) {
	return ParsePositiveInt64(w, value, "invalid "+name)
}

func ParsePositiveInt64(w http.ResponseWriter, value, failureMessage string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id < 1 {
		WriteError(w, http.StatusBadRequest, failureMessage)
		return 0, false
	}
	return id, true
}
