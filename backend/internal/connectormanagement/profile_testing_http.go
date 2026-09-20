package connectormanagement

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const profileConnectionTestTimeout = 20 * time.Second

type ConnectionTestResponse struct {
	TargetID      int64          `json:"target_id"`
	ProfileID     int64          `json:"profile_id"`
	ConnectorKind string         `json:"connector_kind"`
	OK            bool           `json:"ok"`
	Status        string         `json:"status"`
	Message       string         `json:"message,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	DurationMS    int64          `json:"duration_ms"`
}

type ProfileTestingScope struct {
	Database        *sql.DB
	Registry        connectors.Catalog
	Runtime         CredentialRuntimePorts
	AcquireDelivery func(context.Context) (func(), error)
	Admission       *connectors.DeliveryAdmissionIdentity
	SpecialTest     func(context.Context, connectors.TargetView, connectors.CredentialProfileView) (*connectors.ManagementResponse, error)
	RedactDetails   func(context.Context, map[string]any, CredentialBoundary) (map[string]any, error)
}

type ProfileTestingScopeProvider func(http.ResponseWriter) (ProfileTestingScope, bool)

type ProfileTestingHTTPHandler struct{ scope ProfileTestingScopeProvider }

func NewProfileTestingHTTPHandler(scope ProfileTestingScopeProvider) *ProfileTestingHTTPHandler {
	return &ProfileTestingHTTPHandler{scope: scope}
}

func (h *ProfileTestingHTTPHandler) Test(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w)
	if !ok {
		return
	}
	release, ok := acquireLifecycleDelivery(w, r, scope.AcquireDelivery, scope.Admission, "connector connection test was canceled")
	if !ok {
		return
	}
	defer release()
	targetID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	profileID, ok := httptransport.ParsePathInt64(w, r, "profile_id", "profile_id is required")
	if !ok {
		return
	}
	store := connectortargets.NewStore(scope.Database)
	loadedTarget, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	target, profile, err := store.ResolveConnectorActionTarget(
		r.Context(), connectors.FormatTargetRef(loadedTarget.ConnectorKind, targetID, profileID),
	)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	response, specialErr := scope.SpecialTest(r.Context(), target, profile)
	if specialErr != nil {
		httptransport.WriteInternalError(w)
		return
	}
	var testable connectors.TestableConnector
	if response == nil {
		connector, ok := scope.Registry.Get(target.ConnectorKind)
		if !ok {
			httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
			return
		}
		if testable, ok = connector.(connectors.TestableConnector); !ok {
			httptransport.WriteError(w, http.StatusBadRequest, "connector does not support connection tests")
			return
		}
	}
	fullProfile, err := store.GetCredentialProfile(r.Context(), target.ID, profile.ID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	secrets, err := decryptCredentialSecrets(r.Context(), fullProfile, scope.Runtime.DecryptSecret)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	boundary := actionresult.NewCredentialBoundary(secrets)
	if response != nil {
		status, payload, err := ProjectManagementResponse(r.Context(), scope.Runtime, *response, boundary)
		if err != nil {
			httptransport.WriteInternalError(w)
			return
		}
		httptransport.WriteJSON(w, status, payload)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), profileConnectionTestTimeout)
	defer cancel()
	start := time.Now()
	result, err := testable.TestConnection(ctx, scope.Runtime.RuntimeContext(
		loadedTarget, fullProfile, secrets, boundary,
	))
	if err != nil {
		httptransport.WriteJSON(w, http.StatusOK, ConnectionTestResponse{
			TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: target.ConnectorKind,
			Status:     string(connectors.TestUnknownError),
			Message:    boundary.Redact(scope.Runtime.RedactText(r.Context(), err.Error())),
			DurationMS: time.Since(start).Milliseconds(),
		})
		return
	}
	details, err := scope.RedactDetails(r.Context(), result.Details, boundary)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, ConnectionTestResponse{
		TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: target.ConnectorKind,
		OK: result.Status == connectors.TestOK, Status: string(result.Status),
		Message: boundary.Redact(scope.Runtime.RedactText(r.Context(), result.Message)),
		Details: details, DurationMS: time.Since(start).Milliseconds(),
	})
}

func (h *ProfileTestingHTTPHandler) resolve(w http.ResponseWriter) (ProfileTestingScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return ProfileTestingScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return ProfileTestingScope{}, false
	}
	if scope.Database == nil || scope.Registry == nil || !scope.Runtime.valid() || scope.AcquireDelivery == nil || scope.Admission == nil || scope.SpecialTest == nil || scope.RedactDetails == nil {
		httptransport.WriteInternalError(w)
		return ProfileTestingScope{}, false
	}
	return scope, true
}
