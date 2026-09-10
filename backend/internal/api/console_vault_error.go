package api

import (
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

func handleVaultItemError(w http.ResponseWriter, err error) {
	var validation projectvault.ValidationError
	switch {
	case errors.Is(err, connectors.ErrSessionEnvironmentUnsupported):
		writeError(w, http.StatusConflict, "this connector runtime does not support Vault session environments")
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Error())
	case errors.Is(err, projectvault.ErrNotFound):
		writeError(w, http.StatusNotFound, "vault item not found")
	case errors.Is(err, projectvault.ErrStale):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeInternalError(w)
	}
}
