package api

import (
	"context"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) consoleSessionHTTPScope(w http.ResponseWriter) (*connectorapi.LiveConsoleHTTPRuntime, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	return s.infrastructure.LiveConsoleHTTPRuntime(runtime, connectorapi.LiveConsoleHTTPRuntime{
		Principal: func() (gatewayaccess.Principal, error) {
			return s.localExecutionPrincipal(runtime)
		},
		PlanEnvironment: func(ctx context.Context, runtimeID int64, selections []connectorapi.LiveConsoleVaultSelection) (connectorapi.LiveConsoleEnvironmentPlan, error) {
			application, err := s.vaultActionApplication(runtime)
			if err != nil {
				return connectorapi.LiveConsoleEnvironmentPlan{}, err
			}
			items := make([]gatewayvault.SessionSelection, len(selections))
			for index, selection := range selections {
				items[index] = gatewayvault.SessionSelection{
					ItemID: selection.ItemID, SourceProjectID: selection.SourceProjectID,
					ReplaceExisting: selection.ReplaceExisting, BindingID: selection.BindingID,
					BindingRevision: selection.BindingRevision,
				}
			}
			plan, err := application.BuildEnvironmentPlan(ctx, runtimeID, items)
			if err != nil {
				return connectorapi.LiveConsoleEnvironmentPlan{}, err
			}
			ids := make([]int64, 0, len(plan.Items))
			for _, item := range plan.Items {
				ids = append(ids, item.ItemID)
			}
			return connectorapi.LiveConsoleEnvironmentPlan{
				ItemIDs: ids, ContentHash: plan.EnvironmentContentHash, Prepare: plan.Prepare,
			}, nil
		},
		PresentEnvironmentError: presentVaultSessionEnvironmentError,
		ErrorAdapter: func(ctx context.Context, runtimeID int64) connectorapi.ErrorPresenter {
			adapter, _ := s.consoleErrorPresenter(ctx, runtime, runtimeID).(connectorapi.ErrorPresenter)
			return adapter
		},
		CancelForSession: func(ctx context.Context, sessionID int64, errorText string) error {
			requests, err := s.commandRuntime(runtime)
			if err != nil {
				return gatewayoperations.ErrCommandRuntimeUnavailable
			}
			return requests.CancelRunningForSession(ctx, sessionID, errorText)
		},
		RestartRuntime: func(ctx context.Context, runtimeID int64, errorText string) (connectorapi.LiveConsoleRestartResult, error) {
			principal, err := s.localExecutionPrincipal(runtime)
			if err != nil {
				return connectorapi.LiveConsoleRestartResult{}, err
			}
			result, err := s.restartServerConsoleSession(ctx, runtime, principal, runtimeID, errorText)
			return connectorapi.LiveConsoleRestartResult{
				ClosedSessionIDs: result.ClosedSessionIDs, CanceledRunningRequests: result.CanceledRunningRequests,
			}, err
		},
		Observe: func(ctx context.Context, runtimeID int64, action string, payload map[string]any) {
			s.writeObservationAudit(ctx, runtime, "user", nil, runtimeID, action, payload)
		},
		UpgradeWebSocket: s.upgradeWebSocket,
	})
}

func presentVaultSessionEnvironmentError(err error) (int, string, bool) {
	kind, message, ok := gatewayvault.ClassifySessionEnvironmentError(err)
	if !ok {
		return 0, "", false
	}
	switch kind {
	case gatewayvault.SessionEnvironmentValidation:
		return http.StatusBadRequest, message, true
	case gatewayvault.SessionEnvironmentNotFound:
		return http.StatusNotFound, message, true
	case gatewayvault.SessionEnvironmentStale:
		return http.StatusConflict, message, true
	default:
		return 0, "", false
	}
}
