package connectormanagement

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
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
	secrets, err := decryptCredentialSecrets(r.Context(), adminProfile, scope.Runtime.DecryptSecret)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	boundary := actionresult.NewCredentialBoundary(secrets)
	provisioned, err := provisioner.ProvisionCredentialProfile(
		r.Context(), scope.Runtime.RuntimeContext(target, adminProfile, secrets, boundary), request.Input,
	)
	if err != nil {
		writeProvisionError(w, err, boundary.Redact(scope.Runtime.RedactText(r.Context(), err.Error())))
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
	labelExists, err := provisioningProfileLabelExists(r.Context(), store, target.ID, provisioned.Label)
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

	var profile connectortargets.CredentialProfile
	err = scope.WithTransaction(r.Context(), func(tx *sql.Tx, appendAudit AuditAppender) error {
		if tx == nil || appendAudit == nil {
			return errProvisioningRuntimeUnavailable
		}
		txStore := connectortargets.NewTxStore(tx)
		var createErr error
		profile, createErr = txStore.CreateCredentialProfile(r.Context(), connectortargets.CreateCredentialProfileInput{
			TargetID: target.ID, ConnectorKind: target.ConnectorKind,
			Kind: provisioned.Kind, Label: provisioned.Label, Public: provisioned.Public,
			RiskLabel: provisioned.RiskLabel,
		})
		if createErr != nil {
			return createErr
		}
		encrypted, encryptErr := scope.EncryptSecret(r.Context(), profile.ID, serializedSecret)
		if encryptErr != nil {
			return fmt.Errorf("encrypt provisioned credential profile: %w", encryptErr)
		}
		if err := txStore.SetCredentialProfileEncryptedSecret(r.Context(), target.ID, profile.ID, encrypted); err != nil {
			return err
		}
		profile.EncryptedSecretJSON = encrypted
		if err := scope.EnsureRuntimeSurfaces(r.Context(), txStore, target, profile); err != nil {
			return err
		}
		return appendAudit(tx, "user", nil, 0, "connector.profile.provisioned", map[string]any{
			"target_id": target.ID, "profile_id": profile.ID,
			"admin_profile_id": adminProfile.ID, "connector_kind": target.ConnectorKind,
			"kind": profile.Kind, "label": profile.Label,
		})
	})
	if err != nil {
		h.failProvisioned(r.Context(), w, scope, provisioner, target, adminProfile, secrets, provisioned, "profile_persistence", err, provisionFailureTarget)
		return
	}
	httptransport.WriteJSON(w, http.StatusCreated, ProvisionResponse{Profile: ProfileToSummary(profile), Result: provisioned.Result})
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
	outcome := compensateProvisioned(scope, provisioner, target, adminProfile, secrets, provisioned, stage, cause)
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
	writeProvisionFailureCause(w, cause, boundary.Redact(scope.Runtime.RedactText(ctx, cause.Error())))
}

func compensateProvisioned(
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
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), provisionCompensationTimeout)
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
	auditCtx, auditCancel := context.WithTimeout(context.Background(), provisionAuditTimeout)
	auditErr := scope.AuditRequired(auditCtx, action, map[string]any{
		"target_id": target.ID, "admin_profile_id": adminProfile.ID,
		"connector_kind": target.ConnectorKind, "kind": provisioned.Kind, "label": provisioned.Label,
		"failure_stage": stage, "failure": safeProvisionError(boundary, cause),
		"cleanup_status": cleanupStatus, "cleanup_error": safeProvisionError(boundary, cleanupErr),
	})
	auditCancel()
	return provisionCompensationOutcome{cleanupErr: cleanupErr, auditErr: auditErr}
}

func RequireCompletedCredentialCleanup(result connectors.ActionResult, err error) error {
	if err != nil {
		return err
	}
	if result.Status != connectors.ResultCompleted {
		return fmt.Errorf("credential cleanup returned status %q", result.Status)
	}
	return nil
}

func provisioningProfileLabelExists(ctx context.Context, store *connectortargets.Store, targetID int64, label string) (bool, error) {
	profiles, err := store.ListCredentialProfiles(ctx, targetID)
	if err != nil {
		return false, fmt.Errorf("list connector credential profiles: %w", err)
	}
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Label), strings.TrimSpace(label)) {
			return true, nil
		}
	}
	return false, nil
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

func safeProvisionError(boundary actionresult.CredentialBoundary, err error) string {
	if err == nil {
		return ""
	}
	return boundary.Redact(securitypolicy.RedactBasic(err.Error()))
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
	valid := scope.Database != nil && scope.Registry != nil && scope.Runtime.valid() && scope.EncryptSecret != nil &&
		scope.WithTransaction != nil && scope.EnsureRuntimeSurfaces != nil && scope.AuditRequired != nil
	if !valid {
		httptransport.WriteInternalError(w)
		return ProvisioningScope{}, false
	}
	return scope, true
}
