package apiadapter

import (
	"context"
	"net/http"
	"strconv"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

const RoleHistoryOperation = "role-lifecycle-status"

type roleHistoryResponse struct {
	TargetID int64 `json:"target_id"`
	rolejournal.HistoryPage
}

// RunTargetOperation exposes journal inspection to the authenticated local UI.
// The action runtime capability remains resource-only, without secret or dial
// authority. Inspection does not clear a journal fence or repeat a mutation.
func (adapter) RunTargetOperation(ctx context.Context, _ connectorapi.TargetOperationGateway, runtime connectorapi.ConnectorDataRuntime, target connectorapi.Target, operation string, value any) (connectors.ManagementResponse, error) {
	if operation != RoleHistoryOperation {
		return roleHistoryError(http.StatusBadRequest, "unsupported connector operation"), nil
	}
	after, ok := roleHistoryCursor(value)
	if !ok {
		return roleHistoryError(http.StatusBadRequest, "invalid managed Postgres role history request"), nil
	}
	if ctx == nil || resourcecontract.IsNilDependency(runtime) || target.ID < 1 || target.ConnectorKind != postgresconnector.Kind {
		return roleHistoryError(http.StatusConflict, "managed Postgres role history target is unavailable"), nil
	}
	journal := rolejournal.New(runtime.CredentialResources(rolejournal.ResourceKind))
	page, err := journal.HistoryForTarget(ctx, target.ID, after)
	if err != nil {
		return roleHistoryError(http.StatusConflict, "managed Postgres role evidence could not be inspected; reload before retrying"), nil
	}
	return connectors.ManagementResponse{StatusCode: http.StatusOK, Payload: roleHistoryResponse{TargetID: target.ID, HistoryPage: page}}, nil
}

func roleHistoryCursor(value any) (int64, bool) {
	input, ok := value.(map[string]any)
	if !ok || input == nil || len(input) > 1 {
		return 0, false
	}
	if len(input) == 0 {
		return 0, true
	}
	text, ok := input["after_resource_id"].(string)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(text, 10, 64)
	return id, err == nil && id >= 0 && strconv.FormatInt(id, 10) == text
}

func roleHistoryError(status int, message string) connectors.ManagementResponse {
	return connectors.ManagementResponse{StatusCode: status, Payload: map[string]string{"error": message}}
}

var _ connectorapi.TargetOperationRunner = adapter{}
