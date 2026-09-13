package security

import "testing"

func TestNewOwnsSecurityRuntimeState(t *testing.T) {
	state := New(nil)
	if state.PolicyService() == nil || state.RuntimeControlState() == nil || state.VaultLeaseStore() == nil || state.VaultDeliveryCoordinator() == nil {
		t.Fatal("security runtime state is incomplete")
	}
	var nilState *State
	if nilState.PolicyService() != nil || nilState.RuntimeControlState() != nil || nilState.VaultLeaseStore() != nil || nilState.VaultDeliveryCoordinator() != nil {
		t.Fatal("nil security state exposed capabilities")
	}
}
