package gatewayaccess

import (
	"errors"
	"testing"
)

func TestComponentsOwnIndependentAuthenticationState(t *testing.T) {
	first := NewComponent("3210")
	second := NewComponent("3212")
	first.RecordDatabasePasswordFailure("database")
	first.RecordMCPTokenFailure("token")
	if first.DatabasePasswordFailureCount("database") != 1 {
		t.Fatal("first component did not retain its password failure")
	}
	if second.DatabasePasswordFailureCount("database") != 0 {
		t.Fatal("authentication state leaked between components")
	}
}

func TestNilComponentFailsClosed(t *testing.T) {
	var component *Component
	if err := component.WaitDatabasePassword(t.Context(), "database"); !errors.Is(err, ErrComponentUnavailable) {
		t.Fatalf("password wait error = %v", err)
	}
	if err := component.WaitMCPIP(t.Context(), "ip"); !errors.Is(err, ErrComponentUnavailable) {
		t.Fatalf("MCP IP wait error = %v", err)
	}
	if component.AllowVaultReveal("vault") || component.AllowVaultGenerate("vault") || component.AllowVaultRequest("vault") {
		t.Fatal("nil access component allowed a Vault operation")
	}
}
