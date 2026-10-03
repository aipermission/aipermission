package connectormanagement

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectorcredentials"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const (
	provisionCompensationTimeout = 15 * time.Second
	provisionAuditTimeout        = 5 * time.Second
)

type ProvisionRequest struct {
	Input map[string]any `json:"input,omitempty"`
}

type ProvisionResponse struct {
	Profile ProfileSummary          `json:"profile"`
	Result  connectors.ActionResult `json:"result"`
}

type ProvisioningScope struct {
	Database              *sql.DB
	Registry              connectors.Catalog
	Runtime               CredentialRuntimePorts
	AcquireExclusive      func(context.Context) (func(), error)
	Admission             *connectors.DeliveryAdmissionIdentity
	EncryptSecret         func(context.Context, int64, json.RawMessage) (string, error)
	WithTransaction       func(context.Context, func(*sql.Tx, AuditAppender) error) error
	EnsureRuntimeSurfaces func(context.Context, *connectortargets.Store, connectortargets.Target, connectortargets.CredentialProfile) error
	AuditRequired         func(context.Context, string, any) error
}

type ProvisioningScopeProvider func(http.ResponseWriter) (ProvisioningScope, bool)

type ProvisioningHTTPHandler struct{ scope ProvisioningScopeProvider }

type provisionCompensationOutcome struct {
	cleanupErr error
	auditErr   error
}

type provisionFailureResponse uint8

const (
	provisionFailureTarget provisionFailureResponse = iota
	provisionFailureInternal
)

var errProvisioningRuntimeUnavailable = errors.New("credential provisioning runtime is unavailable")

func NewProvisioningHTTPHandler(scope ProvisioningScopeProvider) *ProvisioningHTTPHandler {
	return &ProvisioningHTTPHandler{scope: scope}
}

func (h *ProvisioningHTTPHandler) Provision(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w)
	if !ok {
		return
	}
	targetID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	adminProfileID, ok := httptransport.ParsePathInt64(w, r, "profile_id", "profile_id is required")
	if !ok {
		return
	}
	request := ProvisionRequest{}
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	release, ok := acquireLifecycleMutation(w, r, scope.AcquireExclusive, scope.Admission, "connector credential provisioning was canceled")
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
	adminProfile, err := store.GetCredentialProfile(r.Context(), targetID, adminProfileID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	connector, ok := scope.Registry.Get(target.ConnectorKind)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	provisioner, ok := connector.(connectors.CredentialProvisioner)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "connector does not support credential provisioning")
		return
	}
	runtime, boundary, secrets, err := prepareCredentialOperationRuntime(r.Context(), scope.Runtime, target, adminProfile)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	provisioned, err := provisioner.ProvisionCredentialProfile(r.Context(), runtime, request.Input)
	if err != nil {
		writeProvisionError(w, err, scope.Runtime.RedactCredentialText(r.Context(), err.Error(), boundary))
		return
	}
	boundary.AddStructured(provisioned.Secret)
	if err := validateProvisionedCredentialProfile(connector, provisioned); err != nil {
		h.failProvisioned(r.Context(), w, scope, provisioner, target, adminProfile, secrets, provisioned, "validation", err, provisionFailureTarget)
		return
	}
	redactedResult, err := scope.Runtime.RedactResult(r.Context(), provisioned.Result, boundary)
	if err != nil {
		h.failProvisioned(r.Context(), w, scope, provisioner, target, adminProfile, secrets, provisioned, "result_redaction", err, provisionFailureInternal)
		return
	}
	provisioned.Result = redactedResult
	labelExists, err := store.HasCredentialProfileLabel(r.Context(), target.ID, provisioned.Label)
	if err != nil {
		h.failProvisioned(r.Context(), w, scope, provisioner, target, adminProfile, secrets, provisioned, "profile_label_lookup", err, provisionFailureTarget)
		return
	}
	if labelExists {
		err := connectortargets.ValidationError("connector profile label already exists")
		h.failProvisioned(r.Context(), w, scope, provisioner, target, adminProfile, secrets, provisioned, "duplicate_profile_label", err, provisionFailureTarget)
		return
	}
	serializedSecret, err := json.Marshal(provisioned.Secret)
	if err != nil {
		h.failProvisioned(r.Context(), w, scope, provisioner, target, adminProfile, secrets, provisioned, "secret_encryption", err, provisionFailureInternal)
		return
	}

	publication, err := persistProvisionedProfile(r.Context(), scope, target, adminProfile, provisioned, serializedSecret)
	if err != nil {
		h.failProfilePublication(r.Context(), w, scope, provisioner, target, adminProfile, secrets, provisioned, publication, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusCreated, ProvisionResponse{Profile: ProfileToSummary(publication.profile), Result: provisioned.Result})
}

func (h *ProvisioningHTTPHandler) failProvisioned(
	ctx context.Context,
	w http.ResponseWriter,
	scope ProvisioningScope,
	provisioner connectors.CredentialProvisioner,
	target connectortargets.Target,
	adminProfile connectortargets.CredentialProfile,
	secrets map[string]any,
	provisioned connectors.ProvisionedCredentialProfile,
	stage string,
	cause error,
	response provisionFailureResponse,
) {
	outcome := compensateProvisioned(ctx, scope, provisioner, target, adminProfile, secrets, provisioned, stage, cause)
	if outcome.cleanupErr != nil {
		log.Printf("credential provisioning compensation failed connector=%q target_id=%d stage=%q", target.ConnectorKind, target.ID, stage)
		writeProvisioningStateError(w,
			"credential provisioning failed and remote cleanup could not be confirmed; review the remote service before retrying",
			"provisioning_reconciliation_required",
		)
		return
	}
	if outcome.auditErr != nil {
		log.Printf("credential provisioning compensation audit failed connector=%q target_id=%d stage=%q", target.ConnectorKind, target.ID, stage)
		writeProvisioningStateError(w,
			"credential provisioning cleanup completed but its audit record could not be persisted; review audit health before retrying",
			"provisioning_compensation_audit_failed",
		)
		return
	}
	if response == provisionFailureInternal {
		httptransport.WriteInternalError(w)
		return
	}
	boundary := actionresult.CombinedCredentialBoundary(secrets, provisioned.Secret)
	writeProvisionFailureCause(w, cause, scope.Runtime.RedactCredentialText(ctx, cause.Error(), boundary))
}

func compensateProvisioned(
	requestCtx context.Context,
	scope ProvisioningScope,
	provisioner connectors.CredentialProvisioner,
	target connectortargets.Target,
	adminProfile connectortargets.CredentialProfile,
	secrets map[string]any,
	provisioned connectors.ProvisionedCredentialProfile,
	stage string,
	cause error,
) provisionCompensationOutcome {
	if provisioner == nil {
		return provisionCompensationOutcome{cleanupErr: errors.New("credential provisioner is unavailable")}
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(requestCtx), provisionCompensationTimeout)
	boundary := actionresult.CombinedCredentialBoundary(secrets, provisioned.Secret)
	cleanupResult, cleanupErr := provisioner.CleanupProvisionedCredentialProfile(
		cleanupCtx,
		scope.Runtime.RuntimeContext(target, adminProfile, secrets, boundary),
		connectors.CredentialProfileView{
			TargetID: target.ID, ConnectorKind: target.ConnectorKind,
			Kind: provisioned.Kind, Label: provisioned.Label,
			Public: provisioned.Public, RiskLabel: provisioned.RiskLabel,
		},
	)
	cleanupCancel()
	cleanupErr = RequireCompletedCredentialCleanup(cleanupResult, cleanupErr)
	action := "connector.profile.provisioning_compensated"
	cleanupStatus := "completed"
	if cleanupErr != nil {
		action = "connector.profile.provisioning_reconciliation_required"
		cleanupStatus = "failed"
	}
	auditCtx, auditCancel := context.WithTimeout(context.WithoutCancel(requestCtx), provisionAuditTimeout)
	auditErr := scope.AuditRequired(auditCtx, action, map[string]any{
		"target_id": target.ID, "admin_profile_id": adminProfile.ID,
		"connector_kind": target.ConnectorKind, "kind": provisioned.Kind, "label": provisioned.Label,
		"failure_stage": stage, "failure": connectorcredentials.RedactErrorForAudit(cause, boundary),
		"cleanup_status": cleanupStatus, "cleanup_error": connectorcredentials.RedactErrorForAudit(cleanupErr, boundary),
	})
	auditCancel()
	return provisionCompensationOutcome{cleanupErr: cleanupErr, auditErr: auditErr}
}

func RequireCompletedCredentialCleanup(result connectors.ActionResult, err error) error {
	return connectorcredentials.RequireCompletedCleanup(result, err)
}

func validateProvisionedCredentialProfile(connector connectors.Connector, profile connectors.ProvisionedCredentialProfile) error {
	schema, ok := CredentialSchemaForKind(connector, profile.Kind)
	if !ok {
		return connectortargets.ValidationError("unsupported credential kind")
	}
	if strings.TrimSpace(profile.Label) == "" {
		return connectortargets.ValidationError("profile label is required")
	}
	if err := connectors.ValidateCredentialSchemaValues(schema.Schema, profile.Public, profile.Secret, true); err != nil {
		return connectortargets.ValidationError(err.Error())
	}
	return nil
}

func writeProvisionError(w http.ResponseWriter, err error, safeMessage string) {
	status := http.StatusBadRequest
	if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		status = http.StatusConflict
	}
	httptransport.WriteJSON(w, status, httptransport.ErrorResponse{Error: safeMessage, Code: connectors.ErrorCode(err)})
}

func writeProvisioningStateError(w http.ResponseWriter, message, code string) {
	httptransport.WriteJSON(w, http.StatusInternalServerError, httptransport.ErrorResponse{Error: message, Code: code})
}

func writeProvisionFailureCause(w http.ResponseWriter, err error, safeMessage string) {
	var validation connectortargets.ValidationError
	switch {
	case errors.Is(err, connectortargets.ErrTargetUpdateConflict),
		errors.Is(err, connectortargets.ErrCredentialProfileUpdateConflict):
		httptransport.WriteError(w, http.StatusConflict, safeMessage)
	case errors.Is(err, connectortargets.ErrTargetNotFound),
		errors.Is(err, connectortargets.ErrTargetProfileNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "connector target not found")
	case errors.Is(err, connectortargets.ErrInvalidTargetRef):
		httptransport.WriteError(w, http.StatusBadRequest, "invalid connector target ref")
	case errors.As(err, &validation):
		httptransport.WriteError(w, http.StatusBadRequest, safeMessage)
	default:
		httptransport.WriteInternalError(w)
	}
}

func (h *ProvisioningHTTPHandler) resolve(w http.ResponseWriter) (ProvisioningScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return ProvisioningScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return ProvisioningScope{}, false
	}
	valid := scope.Database != nil && scope.Registry != nil && scope.Runtime.Valid() && scope.AcquireExclusive != nil && scope.EncryptSecret != nil &&
		scope.WithTransaction != nil && scope.EnsureRuntimeSurfaces != nil && scope.AuditRequired != nil
	if !valid {
		httptransport.WriteInternalError(w)
		return ProvisioningScope{}, false
	}
	return scope, true
}
