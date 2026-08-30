package architecture

import (
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
)

const modulePath = "github.com/aipermission/aipermission/backend"

func TestConnectorGroundworkImportBoundaries(t *testing.T) {
	packages := []string{
		modulePath + "/internal/connectors",
		modulePath + "/internal/actions",
		modulePath + "/internal/connectortargets",
	}
	forbidden := append([]string{
		modulePath + "/internal/api",
		modulePath + "/internal/config",
		modulePath + "/internal/db",
		modulePath + "/internal/filetransfer",
		modulePath + "/internal/tokens",
		modulePath + "/internal/vault",
	}, builtInConnectorPackages(t)...)

	for _, pkg := range packages {
		imports := packageDependencies(t, pkg)
		for _, forbiddenImport := range forbidden {
			if importsPackageOrSubpackage(imports, forbiddenImport) {
				t.Fatalf("%s must not import %s", pkg, forbiddenImport)
			}
		}
	}
}

func importsPackageOrSubpackage(imports map[string]bool, root string) bool {
	for imported := range imports {
		if imported == root || strings.HasPrefix(imported, root+"/") {
			return true
		}
	}
	return false
}

func TestBuiltInConnectorCorePackagesStayIndependentFromGatewayState(t *testing.T) {
	forbidden := []string{
		modulePath + "/internal/api",
		modulePath + "/internal/config",
		modulePath + "/internal/connectortargets",
		modulePath + "/internal/db",
		modulePath + "/internal/filetransfer",
		modulePath + "/internal/history",
		modulePath + "/internal/tokens",
		modulePath + "/internal/vault",
	}
	for _, pkg := range builtInConnectorPackages(t) {
		t.Run(strings.TrimPrefix(pkg, modulePath+"/internal/connectors/"), func(t *testing.T) {
			imports := packageDependencies(t, pkg)
			for _, forbiddenImport := range forbidden {
				if importsPackageOrSubpackage(imports, forbiddenImport) {
					t.Fatalf("%s must not import gateway state package %s", pkg, forbiddenImport)
				}
			}
		})
	}
}

func TestBuiltInConnectorImplementationsStayBehindConnectorBoundary(t *testing.T) {
	connectorRoot := modulePath + "/internal/connectors/"
	allowedRegistry := modulePath + "/internal/connectors/builtin"
	builtInPackages := builtInConnectorPackages(t)
	importsByPackage := allPackageImports(t)

	for importer, imports := range importsByPackage {
		if importer == allowedRegistry || strings.HasPrefix(importer, allowedRegistry+"/") || strings.HasPrefix(importer, connectorRoot) {
			continue
		}
		for _, imported := range imports {
			for _, connectorPackage := range builtInPackages {
				if imported == connectorPackage || strings.HasPrefix(imported, connectorPackage+"/") {
					t.Fatalf("%s imports connector implementation %s; shared runtime must use the generic connector boundary", importer, imported)
				}
			}
		}
	}
}

func builtInConnectorPackages(t *testing.T) []string {
	t.Helper()
	registry, err := builtin.NewRegistry()
	if err != nil {
		t.Fatalf("build connector catalog: %v", err)
	}
	packages := make([]string, 0, len(registry.List()))
	for _, info := range registry.List() {
		packages = append(packages, modulePath+"/internal/connectors/"+info.Kind)
	}
	sort.Strings(packages)
	return packages
}

func allPackageImports(t *testing.T) map[string][]string {
	t.Helper()
	cmd := exec.Command("go", "list", "-buildvcs=false", "-f", `{{.ImportPath}}|{{join .Imports " "}}`, "./...")
	cmd.Dir = "../.."
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list ./... failed: %v\n%s", err, string(exitErr.Stderr))
		}
		t.Fatalf("go list ./... failed: %v", err)
	}
	result := map[string][]string{}
	for _, line := range strings.Split(string(output), "\n") {
		importer, imports, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok || importer == "" {
			continue
		}
		result[importer] = strings.Fields(imports)
	}
	return result
}

func packageDependencies(t *testing.T, pkg string) map[string]bool {
	t.Helper()

	cmd := exec.Command("go", "list", "-buildvcs=false", "-deps", "-f", "{{.ImportPath}}", pkg)
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
