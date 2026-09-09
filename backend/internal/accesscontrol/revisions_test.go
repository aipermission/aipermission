package accesscontrol

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/projects"
)

func TestAuthorizationRevisionsIgnorePresentationOrder(t *testing.T) {
	t.Parallel()
	connectorItems := []connectortargets.ActionPermission{
		{TargetID: 2, ProfileID: 3, ActionName: "write"},
		{TargetID: 1, ProfileID: 4, ActionName: "read"},
	}
	connectorReverse := []connectortargets.ActionPermission{connectorItems[1], connectorItems[0]}
	connectorFirst, connectorFirstErr := connectorPermissionsRevision(connectorItems)
	connectorSecond, connectorSecondErr := connectorPermissionsRevision(connectorReverse)
	assertSameRevision(t, connectorFirst, connectorFirstErr, connectorSecond, connectorSecondErr)

	scopeItems := []projects.TokenScope{{ProjectID: 2, Enabled: true}, {ProjectID: 1, Enabled: false}}
	scopeReverse := []projects.TokenScope{scopeItems[1], scopeItems[0]}
	scopeFirst, scopeFirstErr := projectScopesRevision(scopeItems)
	scopeSecond, scopeSecondErr := projectScopesRevision(scopeReverse)
	assertSameRevision(t, scopeFirst, scopeFirstErr, scopeSecond, scopeSecondErr)

	capabilityItems := []Capability{
		{ProjectID: 2, Name: "write"},
		{ProjectID: 1, Name: "read"},
	}
	capabilityReverse := []Capability{capabilityItems[1], capabilityItems[0]}
	capabilityFirst, capabilityFirstErr := projectCapabilitiesRevision(capabilityItems)
	capabilitySecond, capabilitySecondErr := projectCapabilitiesRevision(capabilityReverse)
	assertSameRevision(t, capabilityFirst, capabilityFirstErr, capabilitySecond, capabilitySecondErr)
}

func assertSameRevision(t *testing.T, first string, firstErr error, second string, secondErr error) {
	t.Helper()
	if firstErr != nil || secondErr != nil {
		t.Fatalf("revision errors: first=%v second=%v", firstErr, secondErr)
	}
	if first != second {
		t.Fatalf("revision changed with presentation order: first=%q second=%q", first, second)
	}
}
