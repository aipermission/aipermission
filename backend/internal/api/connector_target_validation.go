package api

import (
	"context"
	"errors"
	"net/http"

	applicationmanagement "github.com/aipermission/aipermission/backend/internal/applicationconnectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (s *Server) validateConnectorTransportConfig(ctx context.Context, store *connectortargets.Store, projectID int64, config map[string]any) error {
	return applicationmanagement.ValidateTransport(ctx, store, projectID, config, func(kind string) bool {
		adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.TCPTransportAdapter)
		return adapter != nil
	})
}

func handleConnectorTargetError(w http.ResponseWriter, err error) {
	var validation connectortargets.ValidationError
	switch {
	case errors.Is(err, connectortargets.ErrTargetUpdateConflict), errors.Is(err, connectortargets.ErrCredentialProfileUpdateConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, connectortargets.ErrTargetNotFound), errors.Is(err, connectortargets.ErrTargetProfileNotFound):
		writeError(w, http.StatusNotFound, "connector target not found")
	case errors.Is(err, connectortargets.ErrInvalidTargetRef):
		writeError(w, http.StatusBadRequest, "invalid connector target ref")
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Error())
	default:
		writeInternalError(w)
	}
}
