package management

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func runCleanupReconciliation(ctx context.Context, gateway connectorapi.TargetOperationGateway, runtime connectorapi.ConnectorDataRuntime, target connectorapi.Target, operation string, value any) (connectors.ManagementResponse, error) {
	input := cleanupAttestInput{}
	if operation == cleanupStatusOperation {
		if err := decodeStrictOperation(value, &struct{}{}); err != nil {
			return managementErrorResponse(http.StatusBadRequest, "invalid cleanup status request"), nil
		}
	} else if err := decodeStrictOperation(value, &input); err != nil {
		return managementErrorResponse(http.StatusBadRequest, "invalid cleanup attestation request"), nil
	}
	snapshot, err := cleanupReconciliationSnapshot(ctx, gateway, runtime, target)
	if err != nil {
		return managementErrorResponse(http.StatusConflict, "SSH cleanup evidence cannot be inspected; verify public profiles, keys and host trust before retrying"), nil
	}
	if operation == cleanupStatusOperation {
		return managementResponse(http.StatusOK, snapshot), nil
	}
	return attestCleanupSnapshot(ctx, gateway, runtime, snapshot, input)
}

func attestCleanupSnapshot(ctx context.Context, gateway connectorapi.TargetOperationGateway, runtime connectorapi.ConnectorDataRuntime, snapshot cleanupReconciliationContext, input cleanupAttestInput) (connectors.ManagementResponse, error) {
	if input.ContextDigest != snapshot.ContextDigest {
		return managementErrorResponse(http.StatusConflict, "SSH cleanup context changed; reload before reconciling"), nil
	}
	for _, view := range snapshot.Records {
		if view.Entry.ResourceID != input.ResourceID {
			continue
		}
		if view.Entry.Record.Generation != input.Generation {
			return managementErrorResponse(http.StatusConflict, "SSH cleanup generation changed; reload before reconciling"), nil
		}
		for _, choice := range view.Choices {
			if choice.Digest != input.IdentityDigest {
				continue
			}
			decision := keycleanup.Attestation{Identity: choice.Identity, ContextDigest: snapshot.ContextDigest, Coverage: input.Coverage, Reason: input.Reason}
			entry, err := keycleanup.New(runtime.CredentialResources(keycleanup.ResourceKind)).Attest(ctx, view.Entry, decision)
			if err != nil {
				return managementErrorResponse(http.StatusConflict, "SSH cleanup evidence was not confirmed; reload and inspect the recorded generation before retrying"), nil
			}
			if err := gateway.ConnectorWriteTargetAudit(ctx, "connector.key_cleanup_attested", map[string]any{
				"target_id": snapshot.TargetID, "resource_id": entry.ResourceID, "generation": entry.Record.Generation,
				"deletion_context_digest": snapshot.ContextDigest, "identity_digest": choice.Digest,
				"subjects": decision.Coverage, "reason": entry.Record.Attestations[len(entry.Record.Attestations)-1].Reason,
			}); err != nil {
				return managementErrorResponse(http.StatusConflict, "SSH cleanup evidence persisted but its audit was not confirmed; reload and inspect the recorded generation"), nil
			}
			return managementResponse(http.StatusOK, map[string]any{"ok": true, "entry": entry}), nil
		}
		return managementErrorResponse(http.StatusConflict, "SSH cleanup identity changed; reload before reconciling"), nil
	}
	return managementErrorResponse(http.StatusConflict, "SSH cleanup record changed; reload before reconciling"), nil
}
