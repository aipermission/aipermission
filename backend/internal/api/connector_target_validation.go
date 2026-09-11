package api

import (
	"context"
	"errors"
	"net/http"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s *Server) validateConnectorTransportConfig(ctx context.Context, runtime databaseRuntime, projectID int64, config map[string]any) error {
	return s.connectorCatalog(runtime).ValidateTargetTransport(ctx, projectID, config)
}

func handleConnectorTargetError(w http.ResponseWriter, err error) {
	var validation connectormgmt.ValidationError
	switch {
	case errors.Is(err, connectormgmt.ErrTargetUpdateConflict), errors.Is(err, connectormgmt.ErrCredentialProfileUpdateConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, connectormgmt.ErrTargetNotFound), errors.Is(err, connectormgmt.ErrTargetProfileNotFound):
		writeError(w, http.StatusNotFound, "connector target not found")
	case errors.Is(err, connectormgmt.ErrInvalidTargetRef):
		writeError(w, http.StatusBadRequest, "invalid connector target ref")
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Error())
	default:
		writeInternalError(w)
	}
}
