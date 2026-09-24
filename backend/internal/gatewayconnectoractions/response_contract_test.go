package gatewayconnectoractions

import (
	"encoding/json"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
)

func TestMCPResponseProjectionsStayIdentical(t *testing.T) {
	for _, status := range []connectors.ResultStatus{connectors.ResultRunning, connectors.ResultCompleted, connectors.ResultFailed} {
		request := connectortargets.ActionRequest{
			ID: 42, Status: status, ConnectorKind: "fixture", TargetID: 3, ProfileID: 4,
			ActionName: "inspect", Output: map[string]any{"ok": true},
		}
		result := connectors.ActionResult{Status: status, Output: map[string]any{"ok": true}}
		resolve := func(connectortargets.ActionRequest) string { return "  poll later  " }
		gateway := MCPResponseFromResult(request, result, resolve)
		mcp := mcpconnector.ResponseFromResult(resolve, request, result)
		gatewayJSON, err := json.Marshal(gateway)
		if err != nil {
			t.Fatal(err)
		}
		mcpJSON, err := json.Marshal(mcp)
		if err != nil {
			t.Fatal(err)
		}
		if string(gatewayJSON) != string(mcpJSON) {
			t.Fatalf("status %s projected differently: %s / %s", status, gatewayJSON, mcpJSON)
		}
	}
}
