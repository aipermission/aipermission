package mcpconnector

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type OutputAuthorization struct {
	Database   *sql.DB
	Tokens     *tokens.Store
	Leases     *vaultsessions.Store
	Delivery   DeliveryGate
	MCPStarted func() bool
	Principal  func(int64) (executionprincipal.Principal, error)
	Now        func() time.Time
}

type DeliveryGate interface {
	Acquire(context.Context) (func(), error)
}

func (authorization *OutputAuthorization) ResponseForToken(
	ctx context.Context,
	adapterRegistry *connectorapi.Registry,
	tokenID int64,
	request connectortargets.ActionRequest,
	result connectors.ActionResult,
) actions.Response {
	response := ResponseFromResult(adapterRegistry, request, result)
	if !authorization.Authorized(ctx, tokenID, request) {
		actions.Withhold(&response)
	}
	return response
}

func (authorization *OutputAuthorization) Authorized(ctx context.Context, tokenID int64, request connectortargets.ActionRequest) bool {
	if !authorization.valid() || request.TokenID == nil || *request.TokenID != tokenID {
		return false
	}
	release, err := authorization.Delivery.Acquire(ctx)
	if err != nil {
		return false
	}
	defer release()
	return authorization.authorizedLocked(ctx, tokenID, request)
}

func (authorization *OutputAuthorization) Deliver(w http.ResponseWriter, r *http.Request, tokenID int64, request connectortargets.ActionRequest, response actions.Response) {
	encoded, err := json.Marshal(response)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	if !authorization.valid() {
		httptransport.WriteInternalError(w)
		return
	}
	release, err := authorization.Delivery.Acquire(r.Context())
	if err != nil {
		return
	}
	defer release()
	if !authorization.authorizedLocked(r.Context(), tokenID, request) {
		actions.Withhold(&response)
		encoded, err = json.Marshal(response)
		if err != nil {
			httptransport.WriteInternalError(w)
			return
		}
	}
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(encoded, '\n'))
}

// authorizedLocked requires the delivery gate to be held by the caller.
func (authorization *OutputAuthorization) authorizedLocked(ctx context.Context, tokenID int64, request connectortargets.ActionRequest) bool {
	if !authorization.MCPStarted() {
		return false
	}
	now := authorization.now().UTC()
	token, err := authorization.Tokens.Get(ctx, tokenID)
	if err != nil || !tokens.Active(token.RevokedAt, token.ExpiresAt, now) {
		return false
	}
	permission, err := connectortargets.NewStore(authorization.Database).GetActionPermission(
		ctx, tokenID, request.TargetID, request.ProfileID, request.ActionName, now,
	)
	if err != nil || permission.ExecutionRule == connectortargets.ActionPermissionBlocked {
		return false
	}
	if request.SessionID == nil && request.SessionGeneration == nil {
		return true
	}
	if request.SessionID == nil || request.SessionGeneration == nil {
		return false
	}
	principal, err := authorization.Principal(tokenID)
	if err != nil {
		return false
	}
	return vaultsessions.NewObserver(authorization.Database, authorization.Leases).Authorized(
		ctx,
		principal,
		vaultsessions.ObserveRequest{SessionID: *request.SessionID, SessionGeneration: *request.SessionGeneration},
	)
}

func (authorization *OutputAuthorization) valid() bool {
	return authorization != nil && authorization.Database != nil && authorization.Tokens != nil && authorization.Leases != nil &&
		authorization.Delivery != nil && authorization.MCPStarted != nil && authorization.Principal != nil
}

func (authorization *OutputAuthorization) now() time.Time {
	if authorization.Now != nil {
		return authorization.Now()
	}
	return time.Now()
}
