package connectormanagement

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type ProfileMutationScope struct {
	Database              *sql.DB
	Registry              connectors.Catalog
	Preparation           CredentialPreparationPorts
	AcquireExclusive      func(context.Context) (func(), error)
	WithTransaction       func(context.Context, func(*sql.Tx, AuditAppender) error) error
	BeforeCreate          func(context.Context, connectortargets.Target) error
	EnsureRuntimeSurfaces func(context.Context, *connectortargets.Store, connectortargets.Target, connectortargets.CredentialProfile) error
	AfterLifecycleChange  func(context.Context, TargetLifecycleChange) error
}

type ProfileMutationScopeProvider func(http.ResponseWriter) (ProfileMutationScope, bool)

type ProfileMutationHTTPHandler struct{ scope ProfileMutationScopeProvider }

var errProfileMutationRuntimeUnavailable = errors.New("connector profile mutation runtime is unavailable")

func NewProfileMutationHTTPHandler(scope ProfileMutationScopeProvider) *ProfileMutationHTTPHandler {
	return &ProfileMutationHTTPHandler{scope: scope}
}

func (h *ProfileMutationHTTPHandler) Create(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, false)
	if !ok {
		return
	}
	targetID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	var request CredentialProfileInput
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	release, ok := acquireLifecycleMutation(w, r, scope.AcquireExclusive, "connector credential profile create was canceled")
	if !ok {
		return
	}
	defer release()
	store := connectortargets.NewStore(scope.Database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	connector, ok := scope.Registry.Get(target.ConnectorKind)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	prepared, err := PrepareCredentialProfile(r.Context(), connector, request, true, nil, "", scope.Preparation)
	if err != nil {
		writeCredentialPreparationError(w, err)
		return
	}

	var profile connectortargets.CredentialProfile
	err = scope.WithTransaction(r.Context(), func(tx *sql.Tx, appendAudit AuditAppender) error {
		if tx == nil || appendAudit == nil {
			return errProfileMutationRuntimeUnavailable
		}
		if err := scope.BeforeCreate(r.Context(), target); err != nil {
			return err
		}
		var createErr error
		profile, createErr = createPreparedCredentialProfile(
			r.Context(), connectortargets.NewTxStore(tx), target, prepared,
			scope.Preparation, scope.EnsureRuntimeSurfaces,
		)
		if createErr != nil {
			return createErr
		}
		return appendAudit(tx, "user", nil, 0, "connector.profile.created", profileAuditPayload(target, profile))
	})
	if err != nil {
		writeTargetError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusCreated, ProfileToSummary(profile))
}

func createPreparedCredentialProfile(
	ctx context.Context,
	store *connectortargets.Store,
	target connectortargets.Target,
	prepared PreparedCredentialProfile,
	preparation CredentialPreparationPorts,
	ensureRuntimeSurfaces func(context.Context, *connectortargets.Store, connectortargets.Target, connectortargets.CredentialProfile) error,
) (connectortargets.CredentialProfile, error) {
	if store == nil || ensureRuntimeSurfaces == nil {
		return connectortargets.CredentialProfile{}, errProfileMutationRuntimeUnavailable
	}
	profile, err := store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: target.ConnectorKind,
		Kind: prepared.Kind, Label: prepared.Label, Public: prepared.Public,
		RiskLabel: prepared.RiskLabel,
	})
	if err != nil {
		return connectortargets.CredentialProfile{}, err
	}
	encrypted, err := EncryptPreparedCredentialSecret(ctx, profile.ID, prepared, preparation)
	if err != nil {
		return connectortargets.CredentialProfile{}, err
	}
	if encrypted != nil {
		if err := store.SetCredentialProfileEncryptedSecret(ctx, target.ID, profile.ID, *encrypted); err != nil {
			return connectortargets.CredentialProfile{}, err
		}
		profile.EncryptedSecretJSON = *encrypted
	}
	if err := ensureRuntimeSurfaces(ctx, store, target, profile); err != nil {
		return connectortargets.CredentialProfile{}, err
	}
	return profile, nil
}

func (h *ProfileMutationHTTPHandler) Update(w http.ResponseWriter, r *http.Request) {
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
	var request CredentialProfileInput
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	release, ok := acquireLifecycleMutation(w, r, scope.AcquireExclusive, "connector credential profile update was canceled")
	if !ok {
		return
	}
	defer release()

	store := connectortargets.NewStore(scope.Database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	connector, ok := scope.Registry.Get(target.ConnectorKind)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	existing, err := store.GetCredentialProfile(r.Context(), targetID, profileID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	if lifecycle, ok := connector.(connectors.ProvisionedCredentialLifecycle); ok {
		request.Public, err = lifecycle.PreserveProvisionedCredentialPublic(connectortargets.CredentialProfileView(existing), request.Public)
		if err != nil {
			writeTargetError(w, connectortargets.ValidationError(err.Error()))
			return
		}
	}
	existingView := connectortargets.CredentialProfileView(existing)
	prepared, err := PrepareCredentialProfile(
		r.Context(), connector, request, request.Secret != nil,
		&existingView, existing.EncryptedSecretJSON, scope.Preparation,
	)
	if err != nil {
		writeCredentialPreparationError(w, err)
		return
	}

	lifecycleChange := TargetLifecycleChange{
		TargetID: target.ID, ProfileID: profileID,
		StaleReason: "connector credential profile changed; send a fresh Vault request",
		UserMessage: "connector credential profile was updated; ask the AI to send a fresh request",
	}
	var profile connectortargets.CredentialProfile
	err = scope.WithTransaction(r.Context(), func(tx *sql.Tx, appendAudit AuditAppender) error {
		if tx == nil || appendAudit == nil {
			return errProfileMutationRuntimeUnavailable
		}
		var updateErr error
		profile, updateErr = UpdatePreparedCredentialProfile(
			r.Context(), connectortargets.NewTxStore(tx), target, existing, prepared,
			scope.Preparation, scope.EnsureRuntimeSurfaces,
		)
		if updateErr != nil {
			return updateErr
		}
		if updateErr := queueLifecycleChange(r.Context(), tx, lifecycleChange); updateErr != nil {
			return updateErr
		}
		return appendAudit(tx, "user", nil, 0, "connector.profile.updated", profileAuditPayload(target, profile))
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
	httptransport.WriteJSON(w, http.StatusOK, ProfileToSummary(profile))
}

func UpdatePreparedCredentialProfile(
	ctx context.Context,
	store *connectortargets.Store,
	target connectortargets.Target,
	existing connectortargets.CredentialProfile,
	prepared PreparedCredentialProfile,
	preparation CredentialPreparationPorts,
	ensureRuntimeSurfaces func(context.Context, *connectortargets.Store, connectortargets.Target, connectortargets.CredentialProfile) error,
) (connectortargets.CredentialProfile, error) {
	if store == nil || ensureRuntimeSurfaces == nil {
		return connectortargets.CredentialProfile{}, errProfileMutationRuntimeUnavailable
	}
	encrypted, err := EncryptPreparedCredentialSecret(ctx, existing.ID, prepared, preparation)
	if err != nil {
		return connectortargets.CredentialProfile{}, err
	}
	profile, err := store.UpdateCredentialProfile(ctx, connectortargets.UpdateCredentialProfileInput{
		TargetID: target.ID, ProfileID: existing.ID, ConnectorKind: target.ConnectorKind,
		Kind: prepared.Kind, Label: prepared.Label, Public: prepared.Public,
		EncryptedSecretJSON: encrypted, ExpectedSecretRevision: &existing.SecretRevision,
		RiskLabel: prepared.RiskLabel,
	})
	if err != nil {
		return connectortargets.CredentialProfile{}, err
	}
	if err := ensureRuntimeSurfaces(ctx, store, target, profile); err != nil {
		return connectortargets.CredentialProfile{}, err
	}
	return profile, nil
}

func (h *ProfileMutationHTTPHandler) resolve(w http.ResponseWriter, update bool) (ProfileMutationScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return ProfileMutationScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return ProfileMutationScope{}, false
	}
	valid := scope.Database != nil && scope.Registry != nil && scope.WithTransaction != nil &&
		scope.EnsureRuntimeSurfaces != nil && scope.AcquireExclusive != nil
	if update {
		valid = valid && scope.AfterLifecycleChange != nil
	} else {
		valid = valid && scope.BeforeCreate != nil
	}
	if !valid {
		httptransport.WriteInternalError(w)
		return ProfileMutationScope{}, false
	}
	return scope, true
}

func writeCredentialPreparationError(w http.ResponseWriter, err error) {
	var inputErr CredentialInputError
	switch {
	case errors.As(err, &inputErr):
		httptransport.WriteError(w, http.StatusBadRequest, inputErr.Error())
	case errors.Is(err, ErrCredentialSecretDecode):
		httptransport.WriteInternalError(w)
	default:
		writeTargetError(w, err)
	}
}

func profileAuditPayload(target connectortargets.Target, profile connectortargets.CredentialProfile) map[string]any {
	return map[string]any{
		"target_id": target.ID, "profile_id": profile.ID, "connector_kind": target.ConnectorKind,
		"kind": profile.Kind, "label": profile.Label,
	}
}
