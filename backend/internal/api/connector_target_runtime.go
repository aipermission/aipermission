package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (s connectorTargetHandlers) invalidateConnectorActionRequestsForTarget(ctx context.Context, runtime *databaseRuntime, targetID int64, profileID int64, reason string, includeRunning bool) (int64, error) {
	if runtime == nil || runtime.database == nil || targetID < 1 {
		return 0, nil
	}
	input := connectortargets.InvalidateActionRequestsForTargetInput{
		TargetID: targetID, ProfileID: profileID,
		Error:         s.redactForPersistence(ctx, runtime, reason),
		RunningError:  s.redactForPersistence(ctx, runtime, "connector configuration changed after dispatch; the external outcome is unknown and must be inspected before retrying"),
		ApprovalDrift: connectorLifecycleApprovalDrift(profileID), IncludeRunning: includeRunning,
	}
	var result connectortargets.InvalidateActionRequestsForTargetResult
	err := s.withAuditedMutation(
		ctx, runtime, "gateway", nil, 0, "connector_action.requests.invalidated",
		func() any {
			return map[string]any{
				"target_id": targetID, "profile_id": profileID,
				"request_ids": result.IDs, "stale_request_ids": result.StaleIDs,
				"outcome_unknown_request_ids": result.OutcomeUnknownIDs, "affected": result.Affected,
			}
		},
		func(tx *sql.Tx) error {
			var err error
			result, err = connectortargets.NewTxStore(tx).InvalidateActionRequestsForTarget(ctx, input)
			if err == nil && result.Affected == 0 {
				return errAuditedMutationUnchanged
			}
			return err
		},
	)
	if errors.Is(err, errAuditedMutationUnchanged) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return result.Affected, nil
}

func (s *Server) ensureConnectorRuntimeSurfacesForProfile(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
	if store == nil {
		return nil
	}
	capabilities := []string{}
	if adapter := s.connectorLiveConsoleTargetAdapterFor(target.ConnectorKind); adapter != nil {
		capabilities = append(capabilities, adapter.LiveConsoleCapabilityKind())
	}
	if s.connectorFileTransferAdapterFor(target.ConnectorKind) != nil {
		capabilities = append(capabilities, connectortargets.RuntimeCapabilityFileTransfer)
	}
	seen := map[string]struct{}{}
	for _, capability := range capabilities {
		if _, exists := seen[capability]; exists {
			continue
		}
		seen[capability] = struct{}{}
		if _, err := store.EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
			ConnectorKind:  target.ConnectorKind,
			TargetID:       target.ID,
			ProfileID:      profile.ID,
			CapabilityKind: capability,
			Label:          profile.Label,
		}); err != nil {
			return err
		}
	}
	return nil
}

func connectorLifecycleApprovalDrift(profileID int64) string {
	if profileID > 0 {
		return "profile"
	}
	return "target"
}

func (s connectorTargetHandlers) runConnectorTargetOperation(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	targetID, ok := parseID(w, r)
	if !ok {
		return
	}
	operation := strings.TrimSpace(r.PathValue("operation"))
	store := connectortargets.NewStore(runtime.database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	adapter := s.connectorTargetOperationRunnerFor(target.ConnectorKind)
	if adapter == nil {
		writeError(w, http.StatusBadRequest, "operation is not supported for this connector")
		return
	}
	adapter.RunTargetOperation(connectorTargetOperationGatewayPort{
		connectorPeerGatewayPort: connectorPeerGatewayPort{server: s.Server},
		handlers:                 s,
		runtime:                  runtime,
		kind:                     target.ConnectorKind,
		targetID:                 target.ID,
	}, w, r, connectorDataRuntimePort(runtime, target.ConnectorKind), target, operation)
}
