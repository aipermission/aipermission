package architecture

import (
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/aipermission/aipermission/backend"

func TestConnectorGroundworkImportBoundaries(t *testing.T) {
	packages := []string{
		modulePath + "/internal/connectors",
		modulePath + "/internal/actions",
	}
	forbidden := []string{
		modulePath + "/internal/api",
		modulePath + "/internal/config",
		modulePath + "/internal/db",
		modulePath + "/internal/execution",
		modulePath + "/internal/filetransfer",
		modulePath + "/internal/servers",
		modulePath + "/internal/sshconfig",
		modulePath + "/internal/sshkeys",
		modulePath + "/internal/tokens",
		modulePath + "/internal/vault",
	}

	for _, pkg := range packages {
		imports := packageDependencies(t, pkg)
		for _, forbiddenImport := range forbidden {
			if imports[forbiddenImport] {
				t.Fatalf("%s must not import %s", pkg, forbiddenImport)
			}
		}
	}
}

func packageDependencies(t *testing.T, pkg string) map[string]bool {
	t.Helper()

	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pkg)
	cmd.Dir = "../.."
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list %s failed: %v\n%s", pkg, err, string(exitErr.Stderr))
		}
		t.Fatalf("go list %s failed: %v", pkg, err)
	}

	imports := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == pkg {
			continue
		}
		imports[line] = true
	}
	return imports
}
