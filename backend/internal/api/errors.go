package api

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/api/httptransport"
)

func writeError(w http.ResponseWriter, status int, message string) {
	writeErrorWithCode(w, status, message, "")
}

func writeErrorWithCode(w http.ResponseWriter, status int, message, code string) {
	httptransport.WriteError(w, status, message, code)
}

func writeInternalError(w http.ResponseWriter) {
	httptransport.WriteInternalError(w)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	return httptransport.DecodeJSON(w, r, target)
}
