package gatewayconnectorapi

import "testing"

func TestPrincipalValidationFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		principal Principal
		valid     bool
	}{
		{name: "local", principal: Principal{Kind: PrincipalLocalOperator, WorkspaceID: "workspace", RuntimeInstanceID: "runtime"}, valid: true},
		{name: "token", principal: Principal{Kind: PrincipalMCPToken, TokenID: 7, WorkspaceID: "workspace", RuntimeInstanceID: "runtime"}, valid: true},
		{name: "missing kind", principal: Principal{WorkspaceID: "workspace", RuntimeInstanceID: "runtime"}},
		{name: "missing workspace", principal: Principal{Kind: PrincipalLocalOperator, RuntimeInstanceID: "runtime"}},
		{name: "missing runtime", principal: Principal{Kind: PrincipalLocalOperator, WorkspaceID: "workspace"}},
		{name: "local token id", principal: Principal{Kind: PrincipalLocalOperator, TokenID: 7, WorkspaceID: "workspace", RuntimeInstanceID: "runtime"}},
		{name: "token missing id", principal: Principal{Kind: PrincipalMCPToken, WorkspaceID: "workspace", RuntimeInstanceID: "runtime"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.principal.Validate(); (err == nil) != test.valid {
				t.Fatalf("validation error = %v, valid=%t", err, test.valid)
			}
		})
	}
}
