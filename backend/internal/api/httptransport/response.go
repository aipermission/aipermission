package httptransport

import (
	"net/http"

	transportcontract "github.com/aipermission/aipermission/backend/internal/httptransport"
)

func WriteJSON(w http.ResponseWriter, status int, payload any) {
	transportcontract.WriteJSON(w, status, payload)
}

func WriteError(w http.ResponseWriter, status int, message, code string) {
	transportcontract.WriteErrorCode(w, status, message, code)
}

func WriteInternalError(w http.ResponseWriter) {
	transportcontract.WriteInternalError(w)
}
