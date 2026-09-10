package api

import (
	"context"
	"errors"
	"net/http"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s *Server) validateConnectorTransportConfig(ctx context.Context, store *connectormgmt.Store, projectID int64, config map[string]any) error {
	return connectormgmt.ValidateTransport(ctx, store, projectID, config, func(kind string) bool {
		adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.TCPTransportAdapter)
		return adapter != nil
	})
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
