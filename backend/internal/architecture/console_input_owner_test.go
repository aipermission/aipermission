package architecture

import (
	"strings"
	"testing"
)

func TestConsoleInputOwnerDoesNotDependOnApplicationServices(t *testing.T) {
	owner := modulePath + "/internal/console/manualinput"
	for dependency := range packageDependencies(t, owner) {
		if dependency != owner && strings.HasPrefix(dependency, modulePath+"/") {
			t.Fatalf("pure input owner must not depend on application service %s", dependency)
		}
	}
}
