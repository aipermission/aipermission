package architecture

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
)

func TestAPIProductionPackagesDoNotImportDatabaseSQL(t *testing.T) {
	apiRoot := modulePath + "/internal/api"
	for importer, imports := range allPackageImports(t) {
		if importer != apiRoot && !strings.HasPrefix(importer, apiRoot+"/") {
			continue
		}
		for _, imported := range imports {
			if imported == "database/sql" {
				t.Errorf("%s imports database/sql; persistence belongs behind a gateway owner port", importer)
			}
		}
	}
}

func TestWorkspaceLifecycleDoesNotOwnBackupPolicy(t *testing.T) {
	for importer, imports := range allPackageImports(t) {
		if importer != modulePath+"/internal/workspacelifecycle" &&
			!strings.HasPrefix(importer, modulePath+"/internal/gatewayworkspace/lifecycle") {
			continue
		}
		for _, imported := range imports {
			if imported == modulePath+"/internal/backups" ||
				strings.HasPrefix(imported, modulePath+"/internal/gatewayoperations/backup") {
				t.Errorf("%s imports %s; backup password policy belongs behind the injected lifecycle port", importer, imported)
			}
		}
	}
}

func TestAPISubpackagesCannotSmuggleDomainDependencies(t *testing.T) {
	apiRoot := modulePath + "/internal/api"
	for importer, imports := range allPackageImports(t) {
		if !strings.HasPrefix(importer, apiRoot+"/") {
			continue
		}
		for _, imported := range imports {
			if strings.HasPrefix(imported, modulePath+"/internal/") &&
				imported != apiRoot && !strings.HasPrefix(imported, apiRoot+"/") {
				t.Errorf("%s imports %s; API child packages must remain transport-only", importer, imported)
			}
		}
	}
}

func TestAPIProductionTypesAreBoundaryOwned(t *testing.T) {
	inspectProductionGoFiles(t, filepath.Join("..", "api"), func(path string, file *ast.File) {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				typeSpec := specification.(*ast.TypeSpec)
				if typeSpec.Assign.IsValid() {
					t.Errorf("%s reexports type %s; API transport contracts must be boundary-owned", path, typeSpec.Name.Name)
				}
			}
		}
	})
}

func TestProductionPackagesDoNotExposeMutableFacades(t *testing.T) {
	inspectProductionGoPackages(t, filepath.Join("..", "..", "internal"), func(path string, file *ast.File, bindings map[string][]ast.Expr) {
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
					initializer := valueInitializer(value, index)
					if mutableFacadeInitializerWithBindings(initializer, bindings, map[string]bool{}) {
						t.Errorf("%s exposes mutable facade %s; declare an owned function or immutable contract", path, identifier.Name)
					}
				}
			}
		}
	})
}

func packageVariableInitializers(file *ast.File) map[string][]ast.Expr {
	bindings := map[string][]ast.Expr{}
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			value := specification.(*ast.ValueSpec)
			for index, identifier := range value.Names {
				if initializer := valueInitializer(value, index); initializer != nil {
					bindings[identifier.Name] = append(bindings[identifier.Name], initializer)
				}
			}
		}
	}
	return bindings
}

type parsedProductionGoFile struct {
	path string
	file *ast.File
}

func inspectProductionGoPackages(t *testing.T, root string, inspect func(string, *ast.File, map[string][]ast.Expr)) {
	t.Helper()
	packages := map[string][]parsedProductionGoFile{}
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
		key := filepath.Dir(path) + "\x00" + file.Name.Name
		packages[key] = append(packages[key], parsedProductionGoFile{path: path, file: file})
		return nil
	})
	if err != nil {
		t.Fatalf("inspect production Go packages in %s: %v", root, err)
	}
	for _, files := range packages {
		bindings := map[string][]ast.Expr{}
		for _, parsed := range files {
			for name, initializers := range packageVariableInitializers(parsed.file) {
				bindings[name] = append(bindings[name], initializers...)
			}
		}
		for _, parsed := range files {
			inspect(parsed.path, parsed.file, bindings)
		}
	}
}

func TestProductionAPIDoesNotConsumeRawWorkspaceScopes(t *testing.T) {
	forbidden := map[string]map[string]bool{
		modulePath + "/internal/gatewayaccess": {
			"AccessScope": true, "AccessScopeProvider": true,
			"MCPScope": true, "MCPScopeProvider": true,
			"MCPActionScope": true, "MCPActionScopeProvider": true,
			"MCPRuntimeScope": true, "MCPRuntimeScopeProvider": true,
			"SecurityHTTPScope": true, "SecurityHTTPScopeProvider": true,
			"ScopeProviders": true,
		},
		modulePath + "/internal/gatewayvault": {
			"HTTPDependencies": true,
			"ProjectScope":     true, "ProjectScopeProvider": true,
			"ProjectVaultHTTPScope": true, "ProjectVaultScopeProvider": true,
			"VaultApprovalHTTPScope": true, "VaultApprovalScopeProvider": true,
			"VaultMCPHTTPScope": true, "MCPVaultScopeProvider": true,
		},
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
		aliases, dotImports := importedPackageAliases(file, forbidden)
		for _, packagePath := range dotImports {
			t.Errorf("%s dot-imports %s; raw owner scopes must remain qualified and enforceable", path, packagePath)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			owner, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			packagePath, tracked := aliases[owner.Name]
			if tracked && forbidden[packagePath][selector.Sel.Name] {
				t.Errorf("%s consumes raw owner scope %s.%s; bind an opaque handler or narrow port in gatewayinfrastructure", path, owner.Name, selector.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("inspect production API scopes: %v", err)
	}
}

func TestOpenAPIRouteSourceHasOneFlagAndOneRead(t *testing.T) {
	file, err := parser.ParseFile(
		token.NewFileSet(),
		filepath.Join("..", "..", "cmd", "openapi", "main.go"),
		nil,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}

	bindings := flagStringBindings(file, "routes")
	if len(bindings) != 1 {
		t.Fatalf("OpenAPI command must declare exactly one routes string flag; found %d", len(bindings))
	}
	reads := dereferencedReadFileCalls(file, bindings[0])
	if reads != 1 {
		t.Fatalf("OpenAPI routes flag %s must flow into exactly one os.ReadFile call; found %d", bindings[0], reads)
	}
}

func TestBuiltInConnectorCatalogMatchesFrontendAndDocs(t *testing.T) {
	backendKinds := builtInConnectorKinds(t)
	sourceKinds := filesystemConnectorKinds(t)
	frontendKinds := frontendConnectorKinds(t)
	documentKinds := documentedConnectorKinds(t)
	assertSameStrings(t, "backend registry", backendKinds, "connector source directories", sourceKinds)
	assertSameStrings(t, "backend registry", backendKinds, "frontend templates", frontendKinds)
	assertSameStrings(t, "backend registry", backendKinds, "generated connector docs", documentKinds)
}

func filesystemConnectorKinds(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "connectors")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	support := map[string]bool{
		"builtin": true, "conformance": true, "connectortest": true,
		"sqlresult": true, "sqlsafe": true,
	}
	kinds := []string{}
	for _, entry := range entries {
		if entry.IsDir() && !support[entry.Name()] {
			kinds = append(kinds, entry.Name())
		}
	}
	sort.Strings(kinds)
	return kinds
}

func importedPackageAliases(file *ast.File, tracked map[string]map[string]bool) (map[string]string, []string) {
	aliases := map[string]string{}
	dotImports := []string{}
	for _, spec := range file.Imports {
		packagePath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || tracked[packagePath] == nil {
			continue
		}
		alias := filepath.Base(packagePath)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias == "." {
			dotImports = append(dotImports, packagePath)
			continue
		}
		if alias != "_" {
			aliases[alias] = packagePath
		}
	}
	return aliases, dotImports
}

func mutableFacadeInitializerWithBindings(expression ast.Expr, bindings map[string][]ast.Expr, visiting map[string]bool) bool {
	return expressionContainsMutableFacade(expression, bindings, visiting, false)
}

func expressionContainsMutableFacade(expression ast.Expr, bindings map[string][]ast.Expr, visiting map[string]bool, directCallee bool) bool {
	if expression == nil {
		return false
	}
	switch value := expression.(type) {
	case *ast.BadExpr, *ast.BasicLit:
		return false
	case *ast.Ident:
		if directCallee || visiting[value.Name] {
			return false
		}
		initializers, ok := bindings[value.Name]
		if !ok {
			return false
		}
		visiting[value.Name] = true
		defer delete(visiting, value.Name)
		for _, initializer := range initializers {
			if expressionContainsMutableFacade(initializer, bindings, visiting, false) {
				return true
			}
		}
		return false
	case *ast.SelectorExpr:
		if directCallee {
			return false
		}
		return true
	case *ast.FuncLit:
		return true
	case *ast.ParenExpr:
		return expressionContainsMutableFacade(value.X, bindings, visiting, directCallee)
	case *ast.Ellipsis:
		return expressionContainsMutableFacade(value.Elt, bindings, visiting, false)
	case *ast.CompositeLit:
		return expressionsContainMutableFacade(value.Elts, bindings, visiting)
	case *ast.IndexExpr:
		return expressionContainsMutableFacade(value.X, bindings, visiting, false) ||
			expressionContainsMutableFacade(value.Index, bindings, visiting, false)
	case *ast.IndexListExpr:
		return expressionContainsMutableFacade(value.X, bindings, visiting, false) ||
			expressionsContainMutableFacade(value.Indices, bindings, visiting)
	case *ast.SliceExpr:
		return expressionContainsMutableFacade(value.X, bindings, visiting, false) ||
			expressionContainsMutableFacade(value.Low, bindings, visiting, false) ||
			expressionContainsMutableFacade(value.High, bindings, visiting, false) ||
			expressionContainsMutableFacade(value.Max, bindings, visiting, false)
	case *ast.TypeAssertExpr:
		return expressionContainsMutableFacade(value.X, bindings, visiting, false)
	case *ast.CallExpr:
		if expressionsContainMutableFacade(value.Args, bindings, visiting) {
			return true
		}
		return expressionContainsMutableFacade(value.Fun, bindings, visiting, true)
	case *ast.StarExpr:
		return expressionContainsMutableFacade(value.X, bindings, visiting, false)
	case *ast.UnaryExpr:
		return expressionContainsMutableFacade(value.X, bindings, visiting, false)
	case *ast.BinaryExpr:
		return expressionContainsMutableFacade(value.X, bindings, visiting, false) ||
			expressionContainsMutableFacade(value.Y, bindings, visiting, false)
	case *ast.KeyValueExpr:
		return expressionContainsMutableFacade(value.Key, bindings, visiting, false) ||
			expressionContainsMutableFacade(value.Value, bindings, visiting, false)
	default:
		return false
	}
}

func expressionsContainMutableFacade(expressions []ast.Expr, bindings map[string][]ast.Expr, visiting map[string]bool) bool {
	for _, expression := range expressions {
		if expressionContainsMutableFacade(expression, bindings, visiting, false) {
			return true
		}
	}
	return false
}

func TestTransportGuardHelpersRejectDisguisedEscapeHatches(t *testing.T) {
	tracked := map[string]map[string]bool{modulePath + "/internal/gatewayaccess": {"AccessScope": true}}
	file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package fixture
import . "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
`, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	_, dotImports := importedPackageAliases(file, tracked)
	if len(dotImports) != 1 || dotImports[0] != modulePath+"/internal/gatewayaccess" {
		t.Fatalf("tracked dot import escaped detection: %v", dotImports)
	}
	for _, source := range []string{
		"owner.Function",
		"(owner.Function)",
		"FunctionType(owner.Function)",
		"func() {}",
		"[]func(){owner.Function}[0]",
		`map[string]func(){"run": owner.Function}["run"]`,
		"struct{ Run func() }{Run: owner.Function}.Run",
	} {
		expression, err := parser.ParseExpr(source)
		if err != nil {
			t.Fatal(err)
		}
		if !mutableFacadeInitializerWithBindings(expression, nil, map[string]bool{}) {
			t.Errorf("mutable facade %q escaped detection", source)
		}
	}
	for _, source := range []string{
		`errors.New("sentinel")`,
		`[]error{errors.New("sentinel")}[0]`,
	} {
		expression, err := parser.ParseExpr(source)
		if err != nil {
			t.Fatal(err)
		}
		if mutableFacadeInitializerWithBindings(expression, nil, map[string]bool{}) {
			t.Errorf("constructor result %q was mistaken for a mutable facade", source)
		}
	}
	bindings := map[string][]ast.Expr{
		"local":    {mustParseExpression(t, "owner.Function")},
		"exported": {mustParseExpression(t, "local")},
	}
	if !mutableFacadeInitializerWithBindings(bindings["exported"][0], bindings, map[string]bool{}) {
		t.Fatal("identifier-chain mutable facade escaped detection")
	}
	crossFileBindings := map[string][]ast.Expr{
		"local": {
			mustParseExpression(t, `errors.New("platform sentinel")`),
			mustParseExpression(t, "[]func(){owner.Function}[0]"),
		},
		"exported": {mustParseExpression(t, "local")},
	}
	if !mutableFacadeInitializerWithBindings(crossFileBindings["exported"][0], crossFileBindings, map[string]bool{}) {
		t.Fatal("nested identifier-chain mutable facade escaped detection")
	}
}

func mustParseExpression(t *testing.T, source string) ast.Expr {
	t.Helper()
	expression, err := parser.ParseExpr(source)
	if err != nil {
		t.Fatal(err)
	}
	return expression
}

func flagStringBindings(file *ast.File, flagName string) []string {
	bindings := []string{}
	ast.Inspect(file, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			return true
		}
		call, ok := assignment.Rhs[0].(*ast.CallExpr)
		if !ok || len(call.Args) < 2 || !isPackageSelector(call.Fun, "flag", "String") {
			return true
		}
		name, ok := stringLiteral(call.Args[0])
		binding, bindingOK := assignment.Lhs[0].(*ast.Ident)
		if ok && bindingOK && name == flagName {
			bindings = append(bindings, binding.Name)
		}
		return true
	})
	return bindings
}

func dereferencedReadFileCalls(file *ast.File, binding string) int {
	count := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || !isPackageSelector(call.Fun, "os", "ReadFile") {
			return true
		}
		dereference, ok := call.Args[0].(*ast.StarExpr)
		identifier, identifierOK := dereference.X.(*ast.Ident)
		if ok && identifierOK && identifier.Name == binding {
			count++
		}
		return true
	})
	return count
}

func isPackageSelector(expression ast.Expr, owner, name string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && identifier.Name == owner
}

func builtInConnectorKinds(t *testing.T) []string {
	t.Helper()
	registry, err := builtin.NewRegistry()
	if err != nil {
		t.Fatalf("build connector catalog: %v", err)
	}
	kinds := make([]string, 0, len(registry.List()))
	for _, info := range registry.List() {
		kinds = append(kinds, info.Kind)
	}
	sort.Strings(kinds)
	return kinds
}

func frontendConnectorKinds(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "frontend", "src", "connectors", "templates")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, entry.Name(), "metadata.json"))
		if err != nil {
			t.Fatalf("read connector metadata for %s: %v", entry.Name(), err)
		}
		var metadata struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(content, &metadata); err != nil {
			t.Fatalf("parse connector metadata for %s: %v", entry.Name(), err)
		}
		if metadata.Kind != entry.Name() {
			t.Errorf("frontend connector directory %s declares kind %q", entry.Name(), metadata.Kind)
		}
		kinds = append(kinds, metadata.Kind)
	}
	sort.Strings(kinds)
	return kinds
}

func documentedConnectorKinds(t *testing.T) []string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "connectors.md"))
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, line := range strings.Split(string(content), "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			continue
		}
		kind := strings.TrimSpace(cells[2])
		if len(kind) >= 2 && strings.HasPrefix(kind, "`") && strings.HasSuffix(kind, "`") {
			kinds = append(kinds, strings.Trim(kind, "`"))
		}
	}
	sort.Strings(kinds)
	return kinds
}

func assertSameStrings(t *testing.T, leftName string, left []string, rightName string, right []string) {
	t.Helper()
	if strings.Join(left, "\x00") != strings.Join(right, "\x00") {
		t.Errorf("%s and %s differ:\n%s: %v\n%s: %v", leftName, rightName, leftName, left, rightName, right)
	}
}

func inspectProductionGoFiles(t *testing.T, root string, inspect func(string, *ast.File)) {
	t.Helper()
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
		inspect(path, file)
		return nil
	})
	if err != nil {
		t.Fatalf("inspect production Go files in %s: %v", root, err)
	}
}

func valueInitializer(value *ast.ValueSpec, index int) ast.Expr {
	if index < len(value.Values) {
		return value.Values[index]
	}
	if len(value.Values) == 1 {
		return value.Values[0]
	}
	return nil
}
