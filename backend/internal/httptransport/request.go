package httptransport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

const DefaultJSONBodyBytes int64 = 1 << 20

func DecodeJSON(w http.ResponseWriter, r *http.Request, target any, maxBytes int64) bool {
	if maxBytes < 1 {
		maxBytes = DefaultJSONBodyBytes
	}
	if err := decodeJSONBody(w, r, target, maxBytes); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			WriteJSON(w, http.StatusRequestEntityTooLarge, ErrorResponse{
				Error: "request body is too large", Code: "request_body_too_large",
			})
			return false
		}
		WriteError(w, http.StatusBadRequest, "invalid json body")
		return false
	}
	return true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any, maxBytes int64) error {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return fmt.Errorf("content type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
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
