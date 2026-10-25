package architecture

import (
	"strings"
	"testing"
)

func TestConsoleInputOwnerDoesNotDependOnApplicationServices(t *testing.T) {
	owner := modulePath + "/internal/console/manualinput"
	for dependency := range packageDependencies(t, owner) {
		if !withinInputOwner(dependency, owner) && strings.HasPrefix(dependency, modulePath+"/") {
			t.Fatalf("pure input owner must not depend on application service %s", dependency)
		}
	}
}

func withinInputOwner(dependency, owner string) bool {
	return dependency == owner || strings.HasPrefix(dependency, owner+"/")
}

func TestInputOwnerNamespaceCannotAdmitApplicationServices(t *testing.T) {
	owner := modulePath + "/internal/console/manualinput"
	for _, dependency := range []string{owner, owner + "/heredocsyntax", owner + "/nested/helper"} {
		if !withinInputOwner(dependency, owner) {
			t.Fatalf("pure child owner rejected: %s", dependency)
		}
	}
	for _, dependency := range []string{owner + "extra", modulePath + "/internal/db", modulePath + "/internal/console"} {
		if withinInputOwner(dependency, owner) {
			t.Fatalf("application service admitted as pure input owner: %s", dependency)
		}
	}
}
