package gatewayconnectorapi

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

func TestConnectorPrincipalContractsShareCanonicalValidation(t *testing.T) {
	principals := []executionprincipal.Principal{
		{},
		{Kind: executionprincipal.KindLocalOperator, WorkspaceID: "workspace", RuntimeInstanceID: "runtime"},
		{Kind: executionprincipal.KindLocalOperator, TokenID: 1, WorkspaceID: "workspace", RuntimeInstanceID: "runtime"},
		{Kind: executionprincipal.KindMCPToken, TokenID: 7, WorkspaceID: "workspace", RuntimeInstanceID: "runtime"},
		{Kind: executionprincipal.KindMCPToken, TokenID: 0, WorkspaceID: "workspace", RuntimeInstanceID: "runtime"},
	}
	for _, principal := range principals {
		canonical := principal.Validate() == nil
		connector := connectors.Principal{
			Kind: connectors.PrincipalKind(principal.Kind), TokenID: principal.TokenID,
			WorkspaceID: principal.WorkspaceID, RuntimeInstanceID: principal.RuntimeInstanceID,
		}
		gateway := Principal{
			Kind: PrincipalKind(principal.Kind), TokenID: principal.TokenID,
			WorkspaceID: principal.WorkspaceID, RuntimeInstanceID: principal.RuntimeInstanceID,
		}
		if (connector.Validate() == nil) != canonical || (gateway.Validate() == nil) != canonical {
			t.Fatalf("principal validation differs for %#v", principal)
		}
	}
	if connectors.ErrInvalidPrincipal.Error() != ErrInvalidPrincipal.Error() {
		t.Fatal("connector principal errors have different public messages")
	}
}
