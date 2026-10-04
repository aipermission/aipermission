package architecture

import "testing"

func TestCredentialInputOwnerUsesOnlyPurePreparationDependencies(t *testing.T) {
	owner := modulePath + "/internal/connectormanagement/profileinput"
	allowed := map[string]bool{
		"context":                           true,
		"errors":                            true,
		"fmt":                               true,
		"strings":                           true,
		modulePath + "/internal/connectors": true,
	}
	imports, found := allPackageImports(t)[owner]
	if !found || len(imports) == 0 {
		t.Fatal("credential input owner missing from supported build graphs")
	}
	for _, dependency := range imports {
		if !allowed[dependency] {
			t.Fatalf("credential input owner must not take state, transport or connector implementation dependency %s", dependency)
		}
	}
}
