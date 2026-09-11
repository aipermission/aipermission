package gatewayvault

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type connectorPolicyStub struct {
	permission connectortargets.ActionPermission
	action     string
	err        error
}

func (stub connectorPolicyStub) SessionEnvironmentVersion(context.Context, int64) (string, error) {
	return "v1", nil
}

func (stub connectorPolicyStub) LiveConsolePermission(context.Context, int64, int64, int64, string) (connectortargets.ActionPermission, string, error) {
	return stub.permission, stub.action, stub.err
}

func (stub connectorPolicyStub) ExpectedPeerIdentities(context.Context, connectortargets.RuntimeSurface) (PeerIdentityExpectation, error) {
	return PeerIdentityExpectation{}, nil
}

func TestActionConnectorPolicyAllowsOnlyPromptOrAlways(t *testing.T) {
	for _, test := range []struct {
		name string
		rule connectortargets.ActionPermissionRule
		ok   bool
	}{
		{name: "prompt", rule: connectortargets.ActionPermissionApprovalRequired, ok: true},
		{name: "always", rule: connectortargets.ActionPermissionAlwaysRun, ok: true},
		{name: "blocked", rule: connectortargets.ActionPermissionBlocked},
		{name: "disabled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			port := actionConnectorPort{delegate: connectorPolicyStub{
				permission: connectortargets.ActionPermission{ExecutionRule: test.rule}, action: " open_console ",
			}}
			permission, action, err := port.LiveConsolePermission(t.Context(), 1, 2, 3, "example")
			if test.ok {
				if err != nil || action != "open_console" || permission.ExecutionRule != test.rule {
					t.Fatalf("allowed policy rejected: permission=%#v action=%q err=%v", permission, action, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "Prompt or Always") {
				t.Fatalf("non-executable policy accepted: permission=%#v action=%q err=%v", permission, action, err)
			}
		})
	}
}

func TestActionConnectorPolicyFailsClosedForInvalidFacts(t *testing.T) {
	sentinel := errors.New("lookup failed")
	for _, test := range []struct {
		name   string
		stub   connectorPolicyStub
		is     error
		phrase string
	}{
		{name: "lookup error", stub: connectorPolicyStub{err: sentinel}, is: sentinel},
		{name: "empty action", stub: connectorPolicyStub{permission: connectortargets.ActionPermission{ExecutionRule: connectortargets.ActionPermissionAlwaysRun}}, phrase: "invalid live console action"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := (actionConnectorPort{delegate: test.stub}).LiveConsolePermission(t.Context(), 1, 2, 3, "example")
			if err == nil || (test.is != nil && !errors.Is(err, test.is)) || (test.phrase != "" && !strings.Contains(err.Error(), test.phrase)) {
				t.Fatalf("unexpected policy error: %v", err)
			}
		})
	}
}
