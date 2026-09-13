package observation

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
)

func TestNewBuildsPolicyBoundRedactor(t *testing.T) {
	withoutPolicy := New(nil, nil, nil, nil, nil, nil, nil, nil)
	if withoutPolicy.PrepareRedactor == nil || withoutPolicy.PrepareRedactor(t.Context()) != nil {
		t.Fatal("nil policy should expose a safe nil redactor")
	}
	policy := securitypolicy.NewService(nil)
	withPolicy := New(nil, nil, func() bool { return true }, policy, nil, nil, nil, nil)
	if withPolicy.MCPStarted == nil || !withPolicy.MCPStarted() {
		t.Fatal("MCP state callback was not preserved")
	}
	if redactor := withPolicy.PrepareRedactor(t.Context()); redactor == nil {
		t.Fatal("policy redactor was not prepared")
	}
}
