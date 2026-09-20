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

type CombinedMutationScope struct {
	Database              *sql.DB
	Registry              connectors.Catalog
	Preparation           CredentialPreparationPorts
	ValidateTransport     func(context.Context, int64, map[string]any) error
	AcquireExclusive      func(context.Context) (func(), error)
	WithTransaction       func(context.Context, func(*sql.Tx, AuditAppender) error) error
	BeforeCreate          func(context.Context, connectortargets.Target) error
	EnsureRuntimeSurfaces func(context.Context, *connectortargets.Store, connectortargets.Target, connectortargets.CredentialProfile) error
	AfterLifecycleChange  func(context.Context, TargetLifecycleChange) error
}

type CombinedMutationScopeProvider func(http.ResponseWriter) (CombinedMutationScope, bool)

type CombinedMutationHTTPHandler struct{ scope CombinedMutationScopeProvider }

var errCombinedMutationRuntimeUnavailable = errors.New("combined connector mutation runtime is unavailable")

func NewCombinedMutationHTTPHandler(scope CombinedMutationScopeProvider) *CombinedMutationHTTPHandler {
	return &CombinedMutationHTTPHandler{scope: scope}
}

func (h *CombinedMutationHTTPHandler) Create(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, false)
	if !ok {
		return
	}
	var request CreateTargetWithProfileRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	connectorKind := strings.TrimSpace(request.Target.ConnectorKind)
	connector, ok := scope.Registry.Get(connectorKind)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	config, err := NormalizeTargetConfig(connector, request.Target.Config)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := scope.ValidateTransport(r.Context(), request.Target.ProjectID, config); err != nil {
		writeTargetError(w, err)
		return
	}
	release, ok := acquireLifecycleMutation(w, r, scope.AcquireExclusive, "connector target create was canceled")
	if !ok {
		return
	}
	defer release()
	prepared, err := PrepareCredentialProfile(r.Context(), connector, request.Profile, true, nil, "", scope.Preparation)
	if err != nil {
		writeCredentialPreparationError(w, err)
		return
	}

	var target connectortargets.Target
	var profile connectortargets.CredentialProfile
	err = scope.WithTransaction(r.Context(), func(tx *sql.Tx, appendAudit AuditAppender) error {
		if tx == nil || appendAudit == nil {
			return errCombinedMutationRuntimeUnavailable
		}
		store := connectortargets.NewTxStore(tx)
		var createErr error
		target, createErr = store.CreateTarget(r.Context(), connectortargets.CreateTargetInput{
			ProjectID: request.Target.ProjectID, ConnectorKind: connectorKind,
			Name: request.Target.Name, Config: config,
		})
		if createErr != nil {
			return createErr
		}
		if createErr := scope.BeforeCreate(r.Context(), target); createErr != nil {
			return createErr
		}
		profile, createErr = createPreparedCredentialProfile(r.Context(), store, target, prepared, scope.Preparation, scope.EnsureRuntimeSurfaces)
		if createErr != nil {
			return createErr
		}
		if createErr := appendAudit(tx, "user", nil, 0, "connector.target.created", targetAuditPayload(target)); createErr != nil {
			return createErr
		}
		return appendAudit(tx, "user", nil, 0, "connector.profile.created", profileAuditPayload(target, profile))
	})
	if err != nil {
		writeTargetError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusCreated, TargetToResponse(target, []connectortargets.CredentialProfile{profile}))
}

func (h *CombinedMutationHTTPHandler) Update(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, true)
	if !ok {
		return
	}
	targetID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	profileID, ok := httptransport.ParsePathInt64(w, r, "profile_id", "profile_id is required")
	if !ok {
		return
	}
	var request UpdateTargetWithProfileRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	release, ok := acquireLifecycleMutation(w, r, scope.AcquireExclusive, "connector target update was canceled")
	if !ok {
		return
	}
	defer release()
	store := connectortargets.NewStore(scope.Database)
	existingTarget, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	connector, ok := scope.Registry.Get(existingTarget.ConnectorKind)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	config, err := NormalizeTargetUpdate(connector, existingTarget.Config, request.Target.Config)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.Target.ProjectID == 0 {
		request.Target.ProjectID = existingTarget.ProjectID
	}
	if err := scope.ValidateTransport(r.Context(), request.Target.ProjectID, config); err != nil {
		writeTargetError(w, err)
		return
	}
	existingProfile, err := store.GetCredentialProfile(r.Context(), targetID, profileID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	if lifecycle, ok := connector.(connectors.ProvisionedCredentialLifecycle); ok {
		request.Profile.Public, err = lifecycle.PreserveProvisionedCredentialPublic(
			connectortargets.CredentialProfileView(existingProfile), request.Profile.Public,
		)
		if err != nil {
			writeTargetError(w, connectortargets.ValidationError(err.Error()))
			return
		}
	}
	existingProfileView := connectortargets.CredentialProfileView(existingProfile)
	prepared, err := PrepareCredentialProfile(
		r.Context(), connector, request.Profile, request.Profile.Secret != nil,
		&existingProfileView, existingProfile.EncryptedSecretJSON, scope.Preparation,
	)
	if err != nil {
		writeCredentialPreparationError(w, err)
		return
	}

	var target connectortargets.Target
	var profile connectortargets.CredentialProfile
	err = scope.WithTransaction(r.Context(), func(tx *sql.Tx, appendAudit AuditAppender) error {
		if tx == nil || appendAudit == nil {
			return errCombinedMutationRuntimeUnavailable
		}
		txStore := connectortargets.NewTxStore(tx)
		var updateErr error
		target, updateErr = txStore.UpdateTarget(r.Context(), connectortargets.UpdateTargetInput{
			ID: targetID, ProjectID: request.Target.ProjectID, Name: request.Target.Name,
			Config: config, ExpectedUpdatedAt: existingTarget.UpdatedAt,
		})
		if updateErr != nil {
			return updateErr
		}
		profile, updateErr = UpdatePreparedCredentialProfile(
			r.Context(), txStore, target, existingProfile, prepared,
			scope.Preparation, scope.EnsureRuntimeSurfaces,
		)
		if updateErr != nil {
			return updateErr
		}
		if updateErr := appendAudit(tx, "user", nil, 0, "connector.target.updated", targetAuditPayload(target)); updateErr != nil {
			return updateErr
		}
		return appendAudit(tx, "user", nil, 0, "connector.profile.updated", profileAuditPayload(target, profile))
	})
	if err != nil {
		writeTargetError(w, err)
		return
	}
	if err := scope.AfterLifecycleChange(r.Context(), TargetLifecycleChange{
		TargetID:    target.ID,
		StaleReason: "connector target or credential profile changed; send a fresh Vault request",
		UserMessage: "connector target or credential profile was updated; ask the AI to send a fresh request",
	}); err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, TargetToResponse(target, []connectortargets.CredentialProfile{profile}))
}

func (h *CombinedMutationHTTPHandler) resolve(w http.ResponseWriter, update bool) (CombinedMutationScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return CombinedMutationScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return CombinedMutationScope{}, false
	}
	valid := scope.Database != nil && scope.Registry != nil && scope.ValidateTransport != nil &&
		scope.WithTransaction != nil && scope.EnsureRuntimeSurfaces != nil && scope.AcquireExclusive != nil
	if update {
		valid = valid && scope.AfterLifecycleChange != nil
	} else {
		valid = valid && scope.BeforeCreate != nil
	}
	if !valid {
		httptransport.WriteInternalError(w)
		return CombinedMutationScope{}, false
	}
	return scope, true
}
