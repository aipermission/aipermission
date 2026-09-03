package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

const (
	defaultJSONBodyBytes         = 1 << 20
	connectorActionJSONBodyBytes = 32 << 20
)

type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeErrorWithCode(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, errorResponse{Error: message, Code: code})
}

func writeInternalError(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := decodeJSONBody(w, r, target); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeErrorWithCode(w, http.StatusRequestEntityTooLarge, "request body is too large", "request_body_too_large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid json body")
		return false
	}
	return true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) error {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return fmt.Errorf("content type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, jsonBodyLimitForPath(r.URL.Path))
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("invalid json body")
	}
	return nil
}

func jsonBodyLimitForPath(path string) int64 {
	switch path {
	case "/api/connector-actions/local-run", "/api/mcp/connector-actions/call":
		return connectorActionJSONBodyBytes
	default:
		return defaultJSONBodyBytes
	}
}
