package access

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func TestConstructorsPreserveAccessCapabilities(t *testing.T) {
	tokenStore := &tokens.Store{}
	policy := securitypolicy.NewService(nil)
	delivery := &vaultsessions.DeliveryCoordinator{}
	leases := vaultsessions.NewStore()
	control := &runtimecontrol.State{}
	if got := NewAccessControl(nil, tokenStore, nil, policy, delivery); got.Tokens != tokenStore || got.Policy != policy || got.Delivery != delivery {
		t.Fatalf("access capability = %#v", got)
	}
	if got := NewMCPAction(nil, tokenStore, leases, delivery, control); got.Leases != leases || got.Control != control {
		t.Fatalf("MCP action capability = %#v", got)
	}
	if got := NewMCPRuntime(control, delivery); got.Control != control || got.Delivery != delivery {
		t.Fatalf("MCP runtime capability = %#v", got)
	}
	if got := NewRuntimeControl(control); got.MCPStarted == nil || got.SetMCPStarted == nil {
		t.Fatal("runtime control methods were not preserved")
	}
	if got := NewConsoleRecovery(&console.Manager{}); got.Recover == nil {
		t.Fatal("console recovery method was not preserved")
	}
	_ = NewVaultMetadata(nil)
	_ = NewMCPRead(nil, nil)
	_ = NewSecurityPolicy(policy)
	_ = NewRuntimeConfiguration(policy, control, nil)
	_ = NewConsoleConfiguration(nil)
	_ = NewMessage(nil)
	_ = NewProject(nil, delivery)
}
