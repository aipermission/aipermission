package connectortargets

import (
	"reflect"
	"testing"
)

func TestActionPermissionRulesExposeClosedPolicySet(t *testing.T) {
	want := []ActionPermissionRule{ActionPermissionAlwaysRun, ActionPermissionApprovalRequired, ActionPermissionBlocked}
	if got := ActionPermissionRules(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ActionPermissionRules() = %v, want %v", got, want)
	}
}

func TestNewActionRequestFilterNormalizesAndNarrowsActiveLookup(t *testing.T) {
	filter, err := NewActionRequestFilter(" approval_pending ", " ssh:4:7 ", " exec ", " true ")
	if err != nil {
		t.Fatal(err)
	}
	want := ActionRequestFilter{
		Status: "approval_pending", ConnectorKind: "ssh", ActionName: "exec",
		TargetID: 4, ProfileID: 7, Active: true, Limit: 1,
	}
	if !reflect.DeepEqual(filter, want) {
		t.Fatalf("filter = %#v, want %#v", filter, want)
	}
}

func TestNewActionRequestFilterRejectsAmbiguousInputs(t *testing.T) {
	for _, test := range []struct {
		name      string
		targetRef string
		action    string
		active    string
	}{
		{name: "invalid active", active: "false"},
		{name: "invalid target", targetRef: "not-a-target"},
		{name: "action without target", action: "exec"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewActionRequestFilter("", test.targetRef, test.action, test.active); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
