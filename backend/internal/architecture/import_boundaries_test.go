package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
)

const modulePath = "github.com/aipermission/aipermission/backend"

const maxTestFileInternalImports = 14

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

func TestConnectorTargetStorageDoesNotDependOnActionService(t *testing.T) {
	storage := modulePath + "/internal/connectortargets"
	actionService := modulePath + "/internal/actions"
	for _, imported := range allPackageImports(t)[storage] {
		if imported == actionService || strings.HasPrefix(imported, actionService+"/") {
			t.Fatalf("%s must satisfy action ports without depending on %s", storage, actionService)
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

func TestExtractedDomainPackagesStayIndependentFromAPI(t *testing.T) {
	packages := []string{
		modulePath + "/internal/accesscontrol",
		modulePath + "/internal/actionresult",
		modulePath + "/internal/actions",
		modulePath + "/internal/backups",
		modulePath + "/internal/commandrequests",
		modulePath + "/internal/connectormanagement",
		modulePath + "/internal/observability",
		modulePath + "/internal/projectvault",
		modulePath + "/internal/retention",
		modulePath + "/internal/retention/sqlstore",
		modulePath + "/internal/console/terminaltext",
		modulePath + "/internal/databasecatalog",
		modulePath + "/internal/gatewayoperations/transfer/httpapi",
		modulePath + "/internal/history",
		modulePath + "/internal/legacymigration",
		modulePath + "/internal/maintenanceconsole",
		modulePath + "/internal/runtimecontrol",
		modulePath + "/internal/securitypolicy",
		modulePath + "/internal/transferjobs",
		modulePath + "/internal/vaultrequests",
	}
	for _, pkg := range packages {
		if importsPackageOrSubpackage(packageDependencies(t, pkg), modulePath+"/internal/api") {
			t.Errorf("%s must not depend on the HTTP/API composition root", pkg)
		}
	}
}

func TestAPIUsesActionApplicationBoundary(t *testing.T) {
	implementation := modulePath + "/internal/actionresult"
	for _, imported := range allPackageImports(t)[modulePath+"/internal/api"] {
		if imported == implementation || strings.HasPrefix(imported, implementation+"/") {
			t.Fatalf("internal/api must consume safe action projections through internal/actions instead of %s", implementation)
		}
	}
}

func TestFileTransferOwnershipBoundary(t *testing.T) {
	for _, imported := range allPackageImports(t)[modulePath+"/internal/api"] {
		if imported == modulePath+"/internal/filetransfer" {
			t.Fatal("internal/api must use the file transfer owner instead of importing its store directly")
		}
	}
}

func TestAPIDependsOnMaintenanceConsolePort(t *testing.T) {
	implementation := modulePath + "/internal/maintenanceconsole"
	if importsPackageOrSubpackage(packageDependencies(t, modulePath+"/internal/api"), implementation) {
		t.Fatalf("internal/api must use the console-domain runtime port instead of importing %s", implementation)
	}
}

func TestAPIDoesNotDependOnProcessConfiguration(t *testing.T) {
	processConfiguration := modulePath + "/internal/config"
	if importsPackageOrSubpackage(packageDependencies(t, modulePath+"/internal/api"), processConfiguration) {
		t.Fatalf("internal/api must consume a narrow runtime configuration contract instead of importing %s", processConfiguration)
	}
}

func TestAPIDependsOnlyOnGatewayBoundariesAndConnectorContract(t *testing.T) {
	apiPackage := modulePath + "/internal/api"
	allowed := map[string]bool{
		modulePath + "/internal/connectors":                 true,
		modulePath + "/internal/gatewayaccess":              true,
		modulePath + "/internal/gatewayconnectoractions":    true,
		modulePath + "/internal/gatewayconnectorapi":        true,
		modulePath + "/internal/gatewayconnectormanagement": true,
		modulePath + "/internal/gatewayinfrastructure":      true,
		modulePath + "/internal/gatewayoperations":          true,
		modulePath + "/internal/gatewayvault":               true,
	}
	for _, imported := range allPackageImports(t)[apiPackage] {
		if strings.HasPrefix(imported, modulePath+"/internal/") && !packageBelongsToAnyRoot(imported, allowed) {
			t.Errorf("internal/api imports %s directly; transport code must use the connector contract or an approved gateway boundary", imported)
		}
	}
}

func packageBelongsToAnyRoot(pkg string, roots map[string]bool) bool {
	for root := range roots {
		if pkg == root || strings.HasPrefix(pkg, root+"/") {
			return true
		}
	}
	return false
}

func TestRetiredGatewayConnectorFacadeStaysAbsent(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", "internal", "gatewayconnectors")); !os.IsNotExist(err) {
		t.Fatalf("internal/gatewayconnectors must remain retired; connector contracts belong to gatewayconnectorapi")
	}
	if _, err := os.Stat(filepath.Join("..", "..", "internal", "connectorapi")); !os.IsNotExist(err) {
		t.Fatalf("internal/connectorapi must remain retired; the canonical connector contract belongs to gatewayconnectorapi")
	}
}

func TestGatewayConnectorContractDoesNotReexportTypes(t *testing.T) {
	root := filepath.Join("..", "..", "internal", "gatewayconnectorapi")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				typeSpec := specification.(*ast.TypeSpec)
				if typeSpec.Assign.IsValid() {
					t.Errorf("%s reexports type %s; connector contracts must be owned at their declaration", path, typeSpec.Name.Name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGatewayBoundariesStayIndependentFromAPI(t *testing.T) {
	apiPackage := modulePath + "/internal/api"
	for importer, imports := range allPackageImports(t) {
		if !strings.HasPrefix(importer, modulePath+"/internal/gateway") {
			continue
		}
		for _, imported := range imports {
			if imported == apiPackage || strings.HasPrefix(imported, apiPackage+"/") {
				t.Errorf("%s must not depend on the HTTP/API composition root", importer)
			}
		}
	}
}

func TestApplicationFacadePackagesStayRetired(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "internal"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "application") {
			t.Errorf("internal/%s reintroduces an application facade; composition belongs in an owning gateway boundary", entry.Name())
		}
	}
}

func TestGatewayBoundariesDoNotExposeMutableValues(t *testing.T) {
	packages := []string{
		"gatewayaccess",
		"gatewayconnectoractions",
		"gatewayconnectorapi",
		"gatewayconnectormanagement",
		"gatewayconnectors",
		"gatewayinfrastructure",
		"gatewayoperations",
		"gatewayvault",
	}
	for _, name := range packages {
		t.Run(name, func(t *testing.T) {
			files, err := filepath.Glob(filepath.Join("..", "..", "internal", name, "*.go"))
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range files {
				if strings.HasSuffix(path, "_test.go") {
					continue
				}
				file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
				if err != nil {
					t.Fatalf("parse %s: %v", path, err)
				}
				for _, declaration := range file.Decls {
					general, ok := declaration.(*ast.GenDecl)
					if !ok || general.Tok != token.VAR {
						continue
					}
					for _, specification := range general.Specs {
						value := specification.(*ast.ValueSpec)
						for _, identifier := range value.Names {
							if identifier.IsExported() && !strings.HasPrefix(identifier.Name, "Err") {
								t.Errorf("%s exposes mutable package variable %s; use a const, type, or function", path, identifier.Name)
							}
						}
					}
				}
			}
		})
	}
}

func TestGatewayBoundariesDoNotReintroduceForwarderFiles(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "internal", "gateway*", "forwarders.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		sort.Strings(paths)
		t.Fatalf("gateway boundaries must organize behavior by ownership instead of generic forwarder files: %v", paths)
	}
}

func TestOpenAPICommandsUseOwnedGatewayRouteSource(t *testing.T) {
	const routeSource = "internal/gatewayinfrastructure/routes.go"
	command, err := os.ReadFile(filepath.Join("..", "..", "cmd", "openapi", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(command), `flag.String("routes", "`+routeSource+`"`) {
		t.Fatalf("OpenAPI command must default to %s", routeSource)
	}
	if strings.Contains(string(command), "internal/api/routes.go") {
		t.Fatal("OpenAPI command references the retired API route source")
	}

	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(contents), routeSource); count != 2 {
		t.Fatalf("Makefile must use %s for both OpenAPI commands; found %d references", routeSource, count)
	}
	if strings.Contains(string(contents), "internal/gatewayroutes/") {
		t.Fatal("Makefile references the retired gatewayroutes package")
	}
}

func TestGatewayBoundariesDoNotExposeConcreteWorkspaceRuntime(t *testing.T) {
	roots, err := filepath.Glob(filepath.Join("..", "..", "internal", "gateway*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			workspaceAliases := map[string]bool{}
			for _, imported := range file.Imports {
				importPath := strings.Trim(imported.Path.Value, `"`)
				if importPath != modulePath+"/internal/workspaceruntime" {
					continue
				}
				name := pathpkg.Base(importPath)
				if imported.Name != nil {
					name = imported.Name.Name
				}
				workspaceAliases[name] = true
			}
			if len(workspaceAliases) == 0 {
				return nil
			}
			for _, declaration := range file.Decls {
				switch typed := declaration.(type) {
				case *ast.FuncDecl:
					if typed.Name.IsExported() && referencesConcreteWorkspaceRuntime(typed.Type, workspaceAliases) {
						t.Errorf("%s exports concrete workspaceruntime.Runtime through %s", path, typed.Name.Name)
					}
				case *ast.GenDecl:
					for _, specification := range typed.Specs {
						typeSpec, ok := specification.(*ast.TypeSpec)
						if ok && typeSpec.Name.IsExported() && referencesConcreteWorkspaceRuntime(typeSpec.Type, workspaceAliases) {
							t.Errorf("%s exports concrete workspaceruntime.Runtime through %s", path, typeSpec.Name.Name)
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("inspect %s: %v", root, err)
		}
	}
}

func TestConcreteWorkspaceRuntimeStaysInsideGatewayFactory(t *testing.T) {
	concreteRuntime := modulePath + "/internal/workspaceruntime"
	allowedImporter := modulePath + "/internal/gatewayworkspace/runtime"
	for importer, imports := range allPackageImports(t) {
		if importer == concreteRuntime || strings.HasPrefix(importer, concreteRuntime+"/") {
			continue
		}
		for _, imported := range imports {
			if imported == concreteRuntime && importer != allowedImporter {
				t.Errorf("%s imports the concrete workspace runtime; only %s may construct it", importer, allowedImporter)
			}
		}
	}
	for _, retired := range []string{"connectors", "gatewayadapter", "observation", "security", "storage"} {
		path := filepath.Join("..", "..", "internal", "workspaceruntime", retired)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s must stay retired; workspace components belong to the gateway runtime boundary", path)
		}
	}
}

func TestGatewayWorkspaceOwnsRuntimeContract(t *testing.T) {
	path := filepath.Join("..", "gatewayworkspace", "runtimecontract", "runtime.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec := specification.(*ast.TypeSpec)
			if typeSpec.Name.Name != "Runtime" {
				continue
			}
			found = true
			if typeSpec.Assign.IsValid() {
				t.Fatal("gateway workspace Runtime must be a boundary-owned interface, not an alias")
			}
			if _, ok := typeSpec.Type.(*ast.InterfaceType); !ok {
				t.Fatal("gateway workspace Runtime must remain an interface")
			}
		}
	}
	if !found {
		t.Fatal("gateway workspace Runtime contract is missing")
	}
	statePackage := modulePath + "/internal/gatewayinfrastructure"
	for _, imported := range allPackageImports(t)[statePackage] {
		if imported == modulePath+"/internal/workspaceruntime" {
			t.Fatalf("%s must store the gateway-owned runtime contract", statePackage)
		}
	}
}

func referencesConcreteWorkspaceRuntime(node ast.Node, workspaceAliases map[string]bool) bool {
	found := false
	ast.Inspect(node, func(candidate ast.Node) bool {
		selector, ok := candidate.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Runtime" {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if ok && workspaceAliases[identifier.Name] {
			found = true
			return false
		}
		return true
	})
	return found
}

func TestBuiltInConnectorImplementationsStayBehindConnectorBoundary(t *testing.T) {
	allowedRegistry := modulePath + "/internal/connectors/builtin"
	builtInPackages := builtInConnectorPackages(t)
	importsByPackage := allPackageImports(t)

	for importer, imports := range importsByPackage {
		if importer == allowedRegistry || strings.HasPrefix(importer, allowedRegistry+"/") {
			continue
		}
		for _, imported := range imports {
			importedOwner := builtInConnectorOwner(imported, builtInPackages)
			if importedOwner == "" {
				continue
			}
			importerOwner := builtInConnectorOwner(importer, builtInPackages)
			if importerOwner == importedOwner {
				continue
			}
			t.Fatalf("%s imports connector implementation %s; shared runtime and sibling connectors must use the generic connector boundary", importer, imported)
		}
	}
}

func TestConnectorPackagesDoNotImportGatewayState(t *testing.T) {
	connectorRoot := modulePath + "/internal/connectors/"
	allowedVaultOwner := modulePath + "/internal/connectors/ssh/sshkeys"
	forbidden := []string{
		modulePath + "/internal/api",
		modulePath + "/internal/config",
		modulePath + "/internal/db",
		modulePath + "/internal/history",
		modulePath + "/internal/tokens",
	}
	for importer, imports := range allPackageImports(t) {
		if !strings.HasPrefix(importer, connectorRoot) {
			continue
		}
		for _, imported := range imports {
			for _, forbiddenImport := range forbidden {
				if imported == forbiddenImport || strings.HasPrefix(imported, forbiddenImport+"/") {
					t.Fatalf("%s directly imports gateway state %s", importer, imported)
				}
			}
			if (imported == modulePath+"/internal/vault" || strings.HasPrefix(imported, modulePath+"/internal/vault/")) && importer != allowedVaultOwner {
				t.Fatalf("%s directly imports Vault state %s; encrypted resource ownership belongs in %s", importer, imported, allowedVaultOwner)
			}
		}
	}
}

func builtInConnectorOwner(pkg string, builtInPackages []string) string {
	for _, connectorPackage := range builtInPackages {
		if pkg == connectorPackage || strings.HasPrefix(pkg, connectorPackage+"/") {
			return connectorPackage
		}
	}
	return ""
}

func TestInternalPackageFanOutBudgets(t *testing.T) {
	importsByPackage := allPackageImports(t)
	const packageBudget = 10
	const ownerBudget = 8
	for importer, imports := range importsByPackage {
		if !strings.HasPrefix(importer, modulePath+"/internal/") {
			continue
		}
		packageCount := 0
		owners := map[string]bool{}
		for _, imported := range imports {
			if strings.HasPrefix(imported, modulePath+"/internal/") {
				packageCount++
				owners[internalDependencyOwner(imported)] = true
			}
		}
		if packageCount > packageBudget {
			t.Errorf("%s has %d direct internal package dependencies; budget is %d", importer, packageCount, packageBudget)
		}
		if len(owners) > ownerBudget {
			t.Errorf("%s depends on %d internal owners; ownership fan-out budget is %d", importer, len(owners), ownerBudget)
		}
	}
}

func internalDependencyOwner(pkg string) string {
	relative := strings.TrimPrefix(pkg, modulePath+"/internal/")
	first, _, _ := strings.Cut(relative, "/")
	if strings.HasPrefix(first, "gateway") {
		return modulePath + "/internal/" + first
	}
	return pkg
}

func TestTestFilesRespectInternalImportBudget(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		count := 0
		for _, imported := range file.Imports {
			if strings.HasPrefix(strings.Trim(imported.Path.Value, `"`), modulePath+"/internal/") {
				count++
			}
		}
		if count > maxTestFileInternalImports {
			t.Errorf("%s has %d direct internal imports; test-file budget is %d", path, count, maxTestFileInternalImports)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect test imports: %v", err)
	}
}

func TestInternalDependencyGraphIsAcyclic(t *testing.T) {
	importsByPackage := allPackageImports(t)
	state := map[string]uint8{}
	stack := []string{}
	var visit func(string)
	visit = func(pkg string) {
		switch state[pkg] {
		case 1:
			cycleStart := 0
			for index, item := range stack {
				if item == pkg {
					cycleStart = index
					break
				}
			}
			t.Fatalf("internal dependency cycle: %s", strings.Join(append(stack[cycleStart:], pkg), " -> "))
		case 2:
			return
		}
		state[pkg] = 1
		stack = append(stack, pkg)
		for _, imported := range importsByPackage[pkg] {
			if strings.HasPrefix(imported, modulePath+"/internal/") {
				visit(imported)
			}
		}
		stack = stack[:len(stack)-1]
		state[pkg] = 2
	}
	for pkg := range importsByPackage {
		if strings.HasPrefix(pkg, modulePath+"/internal/") {
			visit(pkg)
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
		seen := map[string]bool{}
		for _, imported := range strings.Fields(imports) {
			seen[imported] = true
		}
		result[importer] = make([]string, 0, len(seen))
		for imported := range seen {
			result[importer] = append(result[importer], imported)
		}
		sort.Strings(result[importer])
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
