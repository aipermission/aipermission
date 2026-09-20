package connectormanagement

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type AuditAppender func(*sql.Tx, string, *int64, int64, string, any) error

type TargetLifecycleChange struct {
	TargetID       int64
	ProfileID      int64
	StaleReason    string
	UserMessage    string
	IncludeRunning bool
}

type TargetMutationScope struct {
	Database              *sql.DB
	Registry              connectors.Catalog
	ValidateTransport     func(context.Context, int64, map[string]any) error
	AcquireExclusive      func(context.Context) (func(), error)
	WithTransaction       func(context.Context, func(*sql.Tx, AuditAppender) error) error
	EnsureRuntimeSurfaces func(context.Context, *connectortargets.Store, connectortargets.Target, connectortargets.CredentialProfile) error
	AfterLifecycleChange  func(context.Context, TargetLifecycleChange) error
}

type TargetMutationScopeProvider func(http.ResponseWriter) (TargetMutationScope, bool)

type TargetMutationHTTPHandler struct{ scope TargetMutationScopeProvider }

type targetMutationRequirements uint8

const requireTargetUpdateCapabilities targetMutationRequirements = 1

var errTargetMutationRuntimeUnavailable = errors.New("connector target mutation runtime is unavailable")

func NewTargetMutationHTTPHandler(scope TargetMutationScopeProvider) *TargetMutationHTTPHandler {
	return &TargetMutationHTTPHandler{scope: scope}
}

func (h *TargetMutationHTTPHandler) Create(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, 0)
	if !ok {
		return
	}
	var request CreateTargetRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	connectorKind := strings.TrimSpace(request.ConnectorKind)
	connector, ok := scope.Registry.Get(connectorKind)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	config, err := NormalizeTargetConfig(connector, request.Config)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	release, ok := acquireLifecycleMutation(w, r, scope.AcquireExclusive, scope.Admission, "connector target create was canceled")
	if !ok {
		return
	}
	defer release()
	if err := scope.ValidateTransport(r.Context(), request.ProjectID, config); err != nil {
		writeTargetError(w, err)
		return
	}

	var target connectortargets.Target
	err = scope.WithTransaction(r.Context(), func(tx *sql.Tx, appendAudit AuditAppender) error {
		if tx == nil || appendAudit == nil {
			return errTargetMutationRuntimeUnavailable
		}
		var createErr error
		target, createErr = connectortargets.NewTxStore(tx).CreateTarget(r.Context(), connectortargets.CreateTargetInput{
			ProjectID: request.ProjectID, ConnectorKind: connectorKind, Name: request.Name, Config: config,
		})
		if createErr != nil {
			return createErr
		}
		return appendAudit(tx, "user", nil, 0, "connector.target.created", targetAuditPayload(target))
	})
	if err != nil {
		writeTargetError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusCreated, TargetToResponse(target, nil))
}

func (h *TargetMutationHTTPHandler) Update(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireTargetUpdateCapabilities)
	if !ok {
		return
	}
	targetID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	var request UpdateTargetRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	release, ok := acquireLifecycleMutation(w, r, scope.AcquireExclusive, "connector target update was canceled")
	if !ok {
		return
	}
	defer release()

	store := connectortargets.NewStore(scope.Database)
	existing, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	connector, ok := scope.Registry.Get(existing.ConnectorKind)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	config, err := NormalizeTargetUpdate(connector, existing.Config, request.Config)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.ProjectID == 0 {
		request.ProjectID = existing.ProjectID
	}
	if err := scope.ValidateTransport(r.Context(), request.ProjectID, config); err != nil {
		writeTargetError(w, err)
		return
	}

	lifecycleChange := TargetLifecycleChange{
		TargetID: targetID, StaleReason: "connector target changed; send a fresh Vault request",
		UserMessage: "connector target was updated; ask the AI to send a fresh request",
	}
	var target connectortargets.Target
	var profiles []connectortargets.CredentialProfile
	err = scope.WithTransaction(r.Context(), func(tx *sql.Tx, appendAudit AuditAppender) error {
		if tx == nil || appendAudit == nil {
			return errTargetMutationRuntimeUnavailable
		}
		txStore := connectortargets.NewTxStore(tx)
		var updateErr error
		target, updateErr = txStore.UpdateTarget(r.Context(), connectortargets.UpdateTargetInput{
			ID: targetID, ProjectID: request.ProjectID, Name: request.Name, Config: config,
			ExpectedUpdatedAt: existing.UpdatedAt,
		})
		if updateErr != nil {
			return updateErr
		}
		profiles, updateErr = txStore.ListCredentialProfiles(r.Context(), target.ID)
		if updateErr != nil {
			return updateErr
		}
		for _, profile := range profiles {
			if err := scope.EnsureRuntimeSurfaces(r.Context(), txStore, target, profile); err != nil {
				return err
			}
		}
		if updateErr := queueLifecycleChange(r.Context(), tx, lifecycleChange); updateErr != nil {
			return updateErr
		}
		return appendAudit(tx, "user", nil, 0, "connector.target.updated", targetAuditPayload(target))
	})
	if err != nil {
		writeTargetError(w, err)
		return
	}
	if err := finalizeLifecycleMutation(r.Context(), func(ctx context.Context) error {
		return scope.AfterLifecycleChange(ctx, lifecycleChange)
	}); err != nil {
		WriteCommittedLifecycleError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, TargetToResponse(target, profiles))
}

func (h *TargetMutationHTTPHandler) resolve(w http.ResponseWriter, requirements targetMutationRequirements) (TargetMutationScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return TargetMutationScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return TargetMutationScope{}, false
	}
	valid := scope.Database != nil && scope.Registry != nil && scope.ValidateTransport != nil &&
		scope.AcquireExclusive != nil && scope.WithTransaction != nil
	if requirements&requireTargetUpdateCapabilities != 0 {
		valid = valid && scope.EnsureRuntimeSurfaces != nil && scope.AfterLifecycleChange != nil
	}
	if !valid {
		httptransport.WriteInternalError(w)
		return TargetMutationScope{}, false
	}
	return scope, true
}

func targetAuditPayload(target connectortargets.Target) map[string]any {
	return map[string]any{
		"project_id": target.ProjectID, "target_id": target.ID,
		"connector_kind": target.ConnectorKind, "name": target.Name,
	}
}
