package connectormanagement

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectorcredentials"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/transactionstate"
)

type profilePublication struct {
	profile  connectortargets.CredentialProfile
	prepared bool
}

func persistProvisionedProfile(ctx context.Context, scope ProvisioningScope, target connectortargets.Target,
	admin connectortargets.CredentialProfile, provisioned connectors.ProvisionedCredentialProfile, secret json.RawMessage,
) (profilePublication, error) {
	var publication profilePublication
	err := scope.WithTransaction(ctx, func(tx *sql.Tx, appendAudit AuditAppender) error {
		if tx == nil || appendAudit == nil {
			return errProvisioningRuntimeUnavailable
		}
		store := connectortargets.NewTxStore(tx)
		var err error
		publication.profile, err = store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
			TargetID: target.ID, ConnectorKind: target.ConnectorKind,
			Kind: provisioned.Kind, Label: provisioned.Label, Public: provisioned.Public, RiskLabel: provisioned.RiskLabel,
		})
		if err != nil {
			return err
		}
		profile := publication.profile
		encrypted, err := scope.EncryptSecret(ctx, profile.ID, secret)
		if err != nil {
			return fmt.Errorf("encrypt provisioned credential profile: %w", err)
		}
		if err := store.SetCredentialProfileEncryptedSecret(ctx, target.ID, profile.ID, encrypted); err != nil {
			return err
		}
		profile.EncryptedSecretJSON = encrypted
		if err := scope.EnsureRuntimeSurfaces(ctx, store, target, profile); err != nil {
			return err
		}
		if err := appendAudit(tx, "user", nil, 0, "connector.profile.provisioned", map[string]any{
			"target_id": target.ID, "profile_id": profile.ID, "admin_profile_id": admin.ID,
			"connector_kind": target.ConnectorKind, "kind": profile.Kind, "label": profile.Label,
		}); err != nil {
			return err
		}
		snapshot, err := store.GetCredentialProfile(ctx, target.ID, profile.ID)
		if err == nil {
			publication.profile, publication.prepared = snapshot, true
		}
		return err
	})
	return publication, err
}

func (h *ProvisioningHTTPHandler) failProfilePublication(ctx context.Context, w http.ResponseWriter,
	scope ProvisioningScope, provisioner connectors.CredentialProvisioner, target connectortargets.Target,
	admin connectortargets.CredentialProfile, secrets map[string]any, provisioned connectors.ProvisionedCredentialProfile,
	publication profilePublication, cause error,
) {
	if transactionstate.IsNotCommitted(cause) {
		h.failProvisioned(ctx, w, scope, provisioner, target, admin, secrets, provisioned, "profile_persistence", cause, provisionFailureTarget)
		return
	}
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), provisionAuditTimeout)
	var profile connectortargets.CredentialProfile
	var confirmed bool
	if publication.prepared && transactionstate.IsReadbackSafe(cause) {
		profile, confirmed = connectortargets.NewStore(scope.Database).ConfirmCredentialProfilePublication(readCtx, publication.profile)
	}
	cancel()
	if confirmed {
		httptransport.WriteJSON(w, http.StatusCreated, ProvisionResponse{Profile: ProfileToSummary(profile), Result: provisioned.Result})
		return
	}
	// Missing local rows are not proof of rollback. Never destroy a remote
	// credential while a corresponding local commit may have succeeded.
	boundary := actionresult.CombinedCredentialBoundary(secrets, provisioned.Secret)
	auditCtx, cancelAudit := context.WithTimeout(context.WithoutCancel(ctx), provisionAuditTimeout)
	err := scope.AuditRequired(auditCtx, "connector.profile.provisioning_reconciliation_required", map[string]any{
		"target_id": target.ID, "profile_id": publication.profile.ID, "admin_profile_id": admin.ID,
		"connector_kind": target.ConnectorKind, "kind": provisioned.Kind, "label": provisioned.Label,
		"failure_stage": "profile_persistence", "failure": connectorcredentials.RedactErrorForAudit(cause, boundary),
		"cleanup_status": "not_dispatched", "local_persistence_status": "outcome_unknown",
	})
	cancelAudit()
	if err != nil {
		writeProvisioningStateError(w, "credential publication is uncertain and its reconciliation audit could not be persisted; inspect local and remote state before retrying", "provisioning_reconciliation_audit_failed")
		return
	}
	writeProvisionError(w, connectors.ClassifyActionError("profile_persistence_outcome_unknown", connectors.ResultOutcomeUnknown, nil, cause),
		"credential publication is uncertain; no remote cleanup was dispatched; inspect local and remote state before retrying")
}
