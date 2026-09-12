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
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
	"github.com/aipermission/aipermission/backend/internal/maintenancepolicy"
)

const modulePath = "github.com/aipermission/aipermission/backend"

var architecturePolicy = maintenancepolicy.MustLoad().BackendFanout

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
		modulePath + "/internal/api/httptransport":                    true,
		modulePath + "/internal/gatewayaccess/httpowner":              true,
		modulePath + "/internal/gatewayconnectoractions":              true,
		modulePath + "/internal/gatewayconnectorapi":                  true,
		modulePath + "/internal/gatewayconnectormanagement":           true,
		modulePath + "/internal/gatewayinfrastructure":                true,
		modulePath + "/internal/gatewayinfrastructure/connectorports": true,
		modulePath + "/internal/gatewayoperations":                    true,
		modulePath + "/internal/gatewayoperations/transfer":           true,
		modulePath + "/internal/gatewayvault":                         true,
	}
	used := map[string]bool{}
	for _, imported := range allPackageImports(t)[apiPackage] {
		if strings.HasPrefix(imported, modulePath+"/internal/") && !allowed[imported] {
			t.Errorf("internal/api imports %s directly; transport code must use an explicitly approved gateway package", imported)
		}
		used[imported] = true
	}
	for imported := range allowed {
		if !used[imported] {
			t.Errorf("internal/api approved import %s is stale; remove unused boundary allowances", imported)
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

func TestGatewayTypeAliasesAreExplicitCanonicalContracts(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	allowed := map[string]bool{
		"gatewayaccess/access.go:PreparedUISession":                  true,
		"gatewayaccess/access.go:Principal":                          true,
		"gatewayaccess/access.go:SecurityRule":                       true,
		"gatewayaccess/access.go:SecurityRuleInput":                  true,
		"gatewayaccess/access.go:SecuritySettings":                   true,
		"gatewayaccess/access.go:TokenValidationError":               true,
		"gatewayconnectormanagement/management.go:ActionPermission":  true,
		"gatewayconnectormanagement/management.go:ActionRequest":     true,
		"gatewayconnectormanagement/management.go:CredentialProfile": true,
		"gatewayconnectormanagement/management.go:RuntimeSurface":    true,
		"gatewayconnectormanagement/management.go:Target":            true,
		"gatewayconnectormanagement/management.go:ValidationError":   true,
	}
	found := map[string]bool{}
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
		relative = filepath.ToSlash(relative)
		if !strings.HasPrefix(relative, "gateway") {
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
				if !typeSpec.Assign.IsValid() {
					continue
				}
				key := relative + ":" + typeSpec.Name.Name
				if !allowed[key] {
					t.Errorf("%s reexports type %s without an explicit canonical-contract decision", relative, typeSpec.Name.Name)
					continue
				}
				found[key] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect gateway type aliases: %v", err)
	}
	for key := range allowed {
		if !found[key] {
			t.Errorf("stale gateway type-alias allowance %s", key)
		}
	}
}

func TestWorkspaceHandleRemainsOpaque(t *testing.T) {
	path := filepath.Join("..", "gatewayinfrastructure", "infrastructure.go")
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
			if typeSpec.Name.Name != "WorkspaceHandle" {
				continue
			}
			found = true
			structure, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				t.Fatal("WorkspaceHandle must remain an explicit opaque struct")
			}
			for _, field := range structure.Fields.List {
				for _, name := range field.Names {
					if name.IsExported() {
						t.Errorf("WorkspaceHandle exposes mutable owner state through field %s", name.Name)
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("gateway infrastructure is missing WorkspaceHandle")
	}
}

func TestGatewayInfrastructureComponentOnlyComposesOwners(t *testing.T) {
	root := filepath.Join("..", "gatewayinfrastructure")
	want := map[string]bool{
		"AccessOwner": false, "ConnectorActionOwner": false,
		"ConnectorManagementOwner": false, "ConnectorPortsOwner": false,
		"ObservationOwner": false, "OperationsOwner": false,
		"VaultOwner": false, "WorkspaceOwner": false,
	}
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
			method, ok := declaration.(*ast.FuncDecl)
			if !ok || method.Recv == nil || !method.Name.IsExported() {
				continue
			}
			pointer, ok := method.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			receiver, ok := pointer.X.(*ast.Ident)
			if !ok || receiver.Name != "Component" {
				continue
			}
			if _, allowed := want[method.Name.Name]; !allowed {
				t.Errorf("gateway infrastructure Component exposes %s; behavior belongs to a narrow owner capability", method.Name.Name)
				continue
			}
			want[method.Name.Name] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect gateway infrastructure component surface: %v", err)
	}
	for method, found := range want {
		if !found {
			t.Errorf("gateway infrastructure Component is missing owner factory %s", method)
		}
	}
}

func TestGatewayInfrastructureBindsOwnerCapabilitiesWithoutRuntimeLocator(t *testing.T) {
	root := filepath.Join("..", "gatewayinfrastructure")
	componentFile, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "component.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range componentFile.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "resolve" {
			continue
		}
		pointer, ok := function.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		receiver, receiverOK := pointer.X.(*ast.Ident)
		if receiverOK && receiver.Name == "Component" {
			t.Fatal("gateway infrastructure Component reintroduced a runtime service locator")
		}
	}

	infrastructureFile, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "infrastructure.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantCapabilities := map[string]bool{
		"access": false, "connectorActions": false, "connectorManagement": false,
		"connectorPorts": false, "observation": false, "operations": false, "vault": false,
	}
	ast.Inspect(infrastructureFile, func(node ast.Node) bool {
		structure, ok := node.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range structure.Fields.List {
			for _, name := range field.Names {
				if _, tracked := wantCapabilities[name.Name]; tracked {
					wantCapabilities[name.Name] = true
				}
				if name.Name == "ownersByToken" {
					t.Error("gateway infrastructure reintroduced a token-to-runtime registry")
				}
			}
		}
		return true
	})
	for capability, found := range wantCapabilities {
		if !found {
			t.Errorf("WorkspaceHandle is missing bound %s capabilities", capability)
		}
	}

	ownersFile, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "owners.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range ownersFile.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec := specification.(*ast.TypeSpec)
			if typeSpec.Name.Name != "WorkspaceOwner" {
				continue
			}
			if _, ok := typeSpec.Type.(*ast.StructType); !ok || typeSpec.Assign.IsValid() {
				t.Fatal("WorkspaceOwner must remain an opaque wrapper, not a Component-convertible type")
			}
		}
	}
}

func TestAPIResolvesGatewayOwnersOnlyAtCompositionRoot(t *testing.T) {
	want := map[string]bool{
		"AccessOwner": false, "ConnectorActionOwner": false,
		"ConnectorManagementOwner": false, "ConnectorPortsOwner": false,
		"ObservationOwner": false, "OperationsOwner": false,
		"VaultOwner": false, "WorkspaceOwner": false,
	}
	root := filepath.Join("..", "api")
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
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if _, tracked := want[selector.Sel.Name]; !tracked {
				return true
			}
			if filepath.Base(path) != "server.go" {
				t.Errorf("%s resolves %s dynamically; bind gateway owners once in the API composition root", path, selector.Sel.Name)
				return true
			}
			if want[selector.Sel.Name] {
				t.Errorf("API composition root resolves %s more than once", selector.Sel.Name)
			}
			want[selector.Sel.Name] = true
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("inspect API owner composition: %v", err)
	}
	for owner, found := range want {
		if !found {
			t.Errorf("API composition root does not bind %s", owner)
		}
	}
}

func TestProductionAPIDoesNotExposeWorkspaceAdoption(t *testing.T) {
	root := filepath.Join("..", "api")
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
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && function.Name.Name == "NewServer" {
				t.Errorf("%s exposes test-only workspace adoption in production; use NewLockedServer", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect API server constructors: %v", err)
	}
	if _, err := os.Stat(filepath.Join("..", "gatewayinfrastructure", "bootstrap")); !os.IsNotExist(err) {
		t.Error("gateway infrastructure bootstrap dependency bag must remain retired")
	}
}

func TestGatewayInfrastructureDoesNotReturnDatabaseHandles(t *testing.T) {
	root := filepath.Join("..", "gatewayinfrastructure")
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
			method, ok := declaration.(*ast.FuncDecl)
			if !ok || method.Recv == nil || !method.Name.IsExported() || method.Type.Results == nil {
				continue
			}
			for _, result := range method.Type.Results.List {
				pointer, ok := result.Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				selector, ok := pointer.X.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "DB" {
					t.Errorf("%s exposes mutable database handle from Component.%s", path, method.Name.Name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect gateway infrastructure database projections: %v", err)
	}
}

func TestOpenAPICommandsUseOwnedGatewayRouteSource(t *testing.T) {
	const routeSource = "internal/api/httptransport/routes.go"
	command, err := parser.ParseFile(
		token.NewFileSet(),
		filepath.Join("..", "..", "cmd", "openapi", "main.go"),
		nil,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := stringFlagDefault(command, "routes"); !ok || got != routeSource {
		t.Fatalf("OpenAPI command must default to %s", routeSource)
	}
	for _, target := range []string{"rest-contract", "rest-contract-check"} {
		command := exec.Command("make", "-n", target)
		command.Dir = filepath.Join("..", "..", "..")
		output, err := command.Output()
		if err != nil {
			t.Fatalf("dry-run %s: %v", target, err)
		}
		if countCommandArgument(output, "-routes", routeSource) != 1 {
			t.Errorf("make %s must pass exactly one -routes %s argument", target, routeSource)
		}
	}
}

func stringFlagDefault(file *ast.File, name string) (string, bool) {
	var result string
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "String" {
			return true
		}
		owner, ok := selector.X.(*ast.Ident)
		if !ok || owner.Name != "flag" {
			return true
		}
		flagName, nameOK := stringLiteral(call.Args[0])
		value, valueOK := stringLiteral(call.Args[1])
		if nameOK && valueOK && flagName == name {
			result, found = value, true
		}
		return true
	})
	return result, found
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func countCommandArgument(output []byte, option, value string) int {
	fields := strings.Fields(string(output))
	count := 0
	for index := 0; index+1 < len(fields); index++ {
		if fields[index] == option && fields[index+1] == value {
			count++
		}
	}
	return count
}

func TestHTTPPolicyAndAdapterRegistrationStayInTransportOwner(t *testing.T) {
	transportBoundary := filepath.Join("..", "api", "httptransport", "boundary.go")
	if _, err := os.Stat(transportBoundary); err != nil {
		t.Fatalf("HTTP transport boundary is missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join("..", "gatewayoperations", "http_boundary.go")); !os.IsNotExist(err) {
		t.Fatal("gatewayoperations must not own HTTP path classification or browser security policy")
	}

	routes, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "api", "httptransport", "routes.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(routes, func(node ast.Node) bool {
		field, ok := node.(*ast.Field)
		if !ok {
			return true
		}
		for _, name := range field.Names {
			if name.Name == "RegisterAdapterRoutes" {
				t.Error("connector adapters must return declarative routes instead of receiving the HTTP mux")
			}
		}
		return true
	})
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

func TestBackendPackageFanOutBudgets(t *testing.T) {
	importsByPackage := allPackageImports(t)
	for importer, imports := range importsByPackage {
		if !strings.HasPrefix(importer, modulePath+"/internal/") && !strings.HasPrefix(importer, modulePath+"/cmd/") {
			continue
		}
		packageCount := 0
		owners := map[string]bool{}
		for _, imported := range imports {
			if strings.HasPrefix(imported, modulePath+"/internal/") {
				packageCount++
				if strings.HasPrefix(imported, importer+"/") {
					continue
				}
				owners[internalDependencyOwner(imported)] = true
			}
		}
		packageBudget := architecturePolicy.PackageMax
		if override, ok := architecturePolicy.Overrides[importer]; ok {
			packageBudget = override
		}
		if packageCount > packageBudget {
			t.Errorf("%s has %d direct internal package dependencies; budget is %d", importer, packageCount, packageBudget)
		}
		if len(owners) > architecturePolicy.OwnerMax {
			t.Errorf("%s depends on %d internal owners; ownership fan-out budget is %d", importer, len(owners), architecturePolicy.OwnerMax)
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
	root := filepath.Join("..", "..")
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
		if count > architecturePolicy.TestFileInternalImportsMax {
			t.Errorf("%s has %d direct internal imports; test-file budget is %d", path, count, architecturePolicy.TestFileInternalImportsMax)
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
