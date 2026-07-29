package executionprincipal

import "testing"

func TestPrincipalHasNoPermissiveZeroValue(t *testing.T) {
	if err := (Principal{}).Validate(); err == nil {
		t.Fatal("zero principal must be invalid")
	}
	if _, err := MCPToken(0, "workspace", "runtime"); err == nil {
		t.Fatal("token principal without token id must be invalid")
	}
}

func TestPrincipalRuntimeIdentity(t *testing.T) {
	local, err := LocalOperator("workspace", "runtime")
	if err != nil {
		t.Fatal(err)
	}
	token, err := MCPToken(7, "workspace", "runtime")
	if err != nil {
		t.Fatal(err)
	}
	if !local.SameRuntime(token) {
		t.Fatal("principals in the same unlocked runtime should share runtime identity")
	}
}
