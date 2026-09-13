package connectortest_test

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/connectortest"
)

type stableConnector struct{}

func (stableConnector) Kind() string    { return "stable" }
func (stableConnector) Label() string   { return "Stable" }
func (stableConnector) Version() string { return "0.1" }
func (stableConnector) TargetSchema() connectors.Schema {
	return connectors.Schema{}
}
func (stableConnector) CredentialSchemas() []connectors.CredentialSchema { return nil }
func (stableConnector) GetHelp(context.Context, connectors.TargetView) (connectors.ConnectorHelp, error) {
	return connectors.ConnectorHelp{ConnectorID: "stable", Connector: "Stable"}, nil
}
func (stableConnector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{{Name: "inspect", Risk: connectors.RiskRead}}, nil
}
func (stableConnector) PrepareAction(_ context.Context, request connectors.ActionRequest) (connectors.PreparedAction, error) {
	return connectors.PreparedAction{ConnectorKind: "stable", ActionName: request.ActionName, Payload: request.Input}, nil
}
func (stableConnector) ExecuteAction(context.Context, connectors.RuntimeContext, connectors.PreparedAction) (connectors.ActionResult, error) {
	return connectors.ActionResult{Status: connectors.ResultCompleted}, nil
}

func TestConnectorAssertionsAcceptStableContract(t *testing.T) {
	connector := stableConnector{}
	target := connectors.TargetView{ConnectorKind: connector.Kind(), Config: map[string]any{"endpoint": "127.0.0.1"}}
	profile := connectors.CredentialProfileView{ConnectorKind: connector.Kind(), Kind: "token"}
	connectortest.AssertActionListStable(t, connector, target, profile)
	connectortest.AssertPrepareActionDeterministic(t, connector, connectors.ActionRequest{
		Target: target, Profile: profile, ActionName: "inspect", Input: map[string]any{"scope": "status"},
	})
}
