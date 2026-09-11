package api

import (
	"context"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) consoleSessionHTTPScope(w http.ResponseWriter) (*connectorapi.LiveConsoleHTTPRuntime, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	return &connectorapi.LiveConsoleHTTPRuntime{
		Sessions: runtime.ConnectorPort().ConsoleSessionManager(),
		Principal: func() (gatewayaccess.Principal, error) {
			return s.localExecutionPrincipal(runtime)
		},
		PlanEnvironment: func(ctx context.Context, runtimeID int64, selections []gatewayvault.SessionSelection) (connectorapi.LiveConsoleEnvironmentPlan, error) {
			application, err := s.vaultActionApplication(runtime)
			if err != nil {
				return connectorapi.LiveConsoleEnvironmentPlan{}, err
			}
			plan, err := application.BuildEnvironmentPlan(ctx, runtimeID, selections)
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
		ErrorAdapter: func(ctx context.Context, runtimeID int64) connectorapi.ErrorPresenter {
			adapter, _ := s.consoleErrorPresenter(ctx, runtime, runtimeID).(connectorapi.ErrorPresenter)
			return adapter
		},
		CancelForSession: func(ctx context.Context, sessionID int64, errorText string) error {
			if runtime.OperationsPort().CommandRequestRuntime() == nil {
				return gatewayaccess.ErrCommandRuntimeUnavailable
			}
			return runtime.OperationsPort().CommandRequestRuntime().CancelRunningForSession(ctx, sessionID, errorText)
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
	}, true
}
