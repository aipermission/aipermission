package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
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
		modulePath + "/internal/gatewayoperations/transfer/runtime",
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

func TestAPIUsesConnectorActionOwnerBoundary(t *testing.T) {
	for _, implementation := range []string{modulePath + "/internal/actionresult", modulePath + "/internal/actions"} {
		for _, imported := range allPackageImports(t)[modulePath+"/internal/api"] {
			if imported == implementation || strings.HasPrefix(imported, implementation+"/") {
				t.Fatalf("internal/api must consume connector action contracts through gatewayconnectoractions instead of %s", implementation)
			}
		}
	}
}

func TestAPIUsesWorkspaceAndCommandOwnerBoundaries(t *testing.T) {
	forbidden := []string{
		modulePath + "/internal/commandrequests",
		modulePath + "/internal/gatewayworkspace",
	}
	for _, imported := range allPackageImports(t)[modulePath+"/internal/api"] {
		for _, implementation := range forbidden {
			if imported == implementation || strings.HasPrefix(imported, implementation+"/") {
				t.Errorf("internal/api imports %s directly instead of its owning gateway boundary", implementation)
			}
		}
	}
}

func TestGatewayAccessDoesNotOwnCommandRuntime(t *testing.T) {
	imports := allPackageImports(t)[modulePath+"/internal/gatewayaccess"]
	for _, forbidden := range []string{
		modulePath + "/internal/commandrequests",
		modulePath + "/internal/runtimeindex",
	} {
		for _, imported := range imports {
			if imported == forbidden || strings.HasPrefix(imported, forbidden+"/") {
				t.Errorf("gatewayaccess imports %s; command lifecycle belongs to gatewayoperations", forbidden)
			}
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

func TestAPIDependsOnlyOnApprovedGatewayPackages(t *testing.T) {
	apiPackage := modulePath + "/internal/api"
	allowed := map[string]bool{
		modulePath + "/internal/connectors":                           true,
		modulePath + "/internal/gatewayaccess":                        true,
		modulePath + "/internal/gatewayconnectoractions":              true,
		modulePath + "/internal/gatewayconnectorapi":                  true,
		modulePath + "/internal/gatewayconnectormanagement":           true,
		modulePath + "/internal/gatewayinfrastructure":                true,
		modulePath + "/internal/gatewayinfrastructure/connectorports": true,
		modulePath + "/internal/gatewayoperations":                    true,
		modulePath + "/internal/gatewayoperations/backup":             true,
		modulePath + "/internal/gatewayoperations/transfer":           true,
		modulePath + "/internal/gatewayvault":                         true,
	}
	for _, imported := range allPackageImports(t)[apiPackage] {
		if strings.HasPrefix(imported, modulePath+"/internal/") && !allowed[imported] {
			t.Errorf("internal/api imports %s directly; transport code must use an explicitly approved gateway package", imported)
		}
	}
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

func TestConnectorActionBoundaryDoesNotExposeImplementationTypes(t *testing.T) {
	command := exec.Command("go", "doc", "./internal/gatewayconnectoractions")
	command.Dir = "../.."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go doc gatewayconnectoractions: %v\n%s", err, output)
	}
	for _, implementation := range []string{"actions.", "actionresult."} {
		if strings.Contains(string(output), implementation) {
			t.Errorf("gatewayconnectoractions public contract exposes implementation selector %q:\n%s", implementation, output)
		}
	}
}

func TestCommandBoundaryDoesNotExposeCommandRequestTypes(t *testing.T) {
	command := exec.Command("go", "doc", "./internal/gatewayoperations")
	command.Dir = "../.."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go doc gatewayoperations: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "commandrequests.") {
		t.Errorf("gatewayoperations public contract exposes commandrequests implementation types:\n%s", output)
	}
}

func TestGatewayVaultOwnsTransportContracts(t *testing.T) {
	path := filepath.Join("..", "gatewayvault", "vault.go")
	want := map[string]bool{
		"ProjectScope": true, "ProjectVaultHTTPScope": true,
		"VaultRequestApplication": true, "VaultApprovalHTTPScope": true,
		"VaultMCPHTTPScope": true, "RequestInvalidator": true,
		"SessionMutationScope": true, "SessionReference": true,
		"SessionSelection": true, "VaultSessionReference": true,
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec := specification.(*ast.TypeSpec)
			if !want[typeSpec.Name.Name] {
				continue
			}
			delete(want, typeSpec.Name.Name)
			if typeSpec.Assign.IsValid() {
				t.Errorf("%s reexports %s; Vault transport contracts must be boundary-owned", path, typeSpec.Name.Name)
			}
		}
	}
	for name := range want {
		t.Errorf("%s is missing boundary-owned contract %s", path, name)
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
	root := filepath.Join("..", "..", "internal")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		owner := strings.Split(filepath.ToSlash(relative), "/")[0]
		if !strings.HasPrefix(owner, "gateway") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, specification := range general.Specs {
				value := specification.(*ast.ValueSpec)
				for index, identifier := range value.Names {
					if !identifier.IsExported() {
						continue
					}
					initializer := ast.Expr(nil)
					if index < len(value.Values) {
						initializer = value.Values[index]
					} else if len(value.Values) == 1 {
						initializer = value.Values[0]
					}
					_, selectorAlias := initializer.(*ast.SelectorExpr)
					if !strings.HasPrefix(identifier.Name, "Err") || selectorAlias {
						t.Errorf("%s exposes mutable package variable %s; owned sentinel errors may use errors.New, all other values must use a const, type, or function", path, identifier.Name)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect gateway mutable values: %v", err)
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

func TestConcreteWorkspaceRuntimeStaysInsideGatewayOwner(t *testing.T) {
	concreteRuntime := modulePath + "/internal/workspaceruntime"
	allowedImporter := modulePath + "/internal/gatewayworkspace"
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

func TestGatewayWorkspaceOwnsExplicitRuntimeComposition(t *testing.T) {
	for _, retired := range []string{
		filepath.Join("..", "gatewayworkspace", "runtimecontract"),
		filepath.Join("..", "gatewayworkspace", "runtime", "runtime.go"),
	} {
		if _, err := os.Stat(retired); !os.IsNotExist(err) {
			t.Errorf("%s must remain retired; workspace composition belongs to gatewayworkspace.Runtime", retired)
		}
	}

	path := filepath.Join("..", "gatewayworkspace", "workspace.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantFields := map[string]bool{"Identity": false, "Storage": false, "Connectors": false, "Security": false, "Observation": false, "owner": false}
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
			structure, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				t.Fatal("gateway workspace Runtime must be an explicit composition struct")
			}
			for _, field := range structure.Fields.List {
				for _, name := range field.Names {
					if _, tracked := wantFields[name.Name]; tracked {
						wantFields[name.Name] = true
					}
				}
			}
		}
	}
	for field, found := range wantFields {
		if !found {
			t.Errorf("gateway workspace Runtime composition is missing %s", field)
		}
	}
}

func TestConcreteWorkspaceRuntimeHasNoServiceLocatorGetters(t *testing.T) {
	path := filepath.Join("..", "workspaceruntime", "runtime.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"StoragePort": true, "ConnectorPort": true, "SecurityPort": true, "ObservationPort": true,
		"WorkspaceIdentifier": true, "RuntimeIdentifier": true, "DatabaseIdentifier": true,
		"DatabasePath": true, "GatewaySecretValue": true, "UIRetryIdentifier": true,
		"ActionIdentity": true, "IdentityReady": true, "IsMCPStarted": true,
	}
	for _, declaration := range file.Decls {
		method, ok := declaration.(*ast.FuncDecl)
		if ok && method.Recv != nil && forbidden[method.Name.Name] {
			t.Errorf("concrete workspace runtime exposes service-locator method %s", method.Name.Name)
		}
	}
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
	// Composition packages may import multiple explicitly approved packages from
	// one owner; the stricter owner budget below prevents boundary sprawl.
	const packageBudget = 11
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
