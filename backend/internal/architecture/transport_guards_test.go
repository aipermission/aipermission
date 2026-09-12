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
		aliases := importedPackageAliases(file, forbidden)
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
	frontendKinds := frontendConnectorKinds(t)
	documentKinds := documentedConnectorKinds(t)
	assertSameStrings(t, "backend registry", backendKinds, "frontend templates", frontendKinds)
	assertSameStrings(t, "backend registry", backendKinds, "generated connector docs", documentKinds)
}

func importedPackageAliases(file *ast.File, tracked map[string]map[string]bool) map[string]string {
	aliases := map[string]string{}
	for _, spec := range file.Imports {
		packagePath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || tracked[packagePath] == nil {
			continue
		}
		alias := filepath.Base(packagePath)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias != "_" && alias != "." {
			aliases[alias] = packagePath
		}
	}
	return aliases
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
