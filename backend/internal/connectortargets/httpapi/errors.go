// Package httpapi owns HTTP presentation of connector target storage errors.
// Storage and connector implementations remain independent of HTTP concerns.
package httpapi

import (
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

// WriteError preserves the read boundary without exposing mutation conflicts.
func WriteError(w http.ResponseWriter, err error) {
	var validation connectortargets.ValidationError
	switch {
	case errors.Is(err, connectortargets.ErrTargetNotFound), errors.Is(err, connectortargets.ErrTargetProfileNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "connector target not found")
	case errors.Is(err, connectortargets.ErrInvalidTargetRef):
		httptransport.WriteError(w, http.StatusBadRequest, "invalid connector target ref")
	case errors.As(err, &validation):
		httptransport.WriteError(w, http.StatusBadRequest, validation.Error())
	default:
		httptransport.WriteInternalError(w)
	}
}

// WriteManagementError retains target/profile conflict precedence for local
// management and permission handlers, while sharing all other presentation.
func WriteManagementError(w http.ResponseWriter, err error) {
	if errors.Is(err, connectortargets.ErrTargetUpdateConflict) || errors.Is(err, connectortargets.ErrCredentialProfileUpdateConflict) {
		httptransport.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	WriteError(w, err)
}
