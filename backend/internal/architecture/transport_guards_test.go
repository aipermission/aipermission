package architecture

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
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
	sharedTransport := modulePath + "/internal/httptransport"
	for importer, imports := range allPackageImports(t) {
		if !strings.HasPrefix(importer, apiRoot+"/") {
			continue
		}
		for _, imported := range imports {
			if strings.HasPrefix(imported, modulePath+"/internal/") &&
				imported != sharedTransport && imported != apiRoot && !strings.HasPrefix(imported, apiRoot+"/") {
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
					if identifier.Name == "_" {
						continue
					}
					initializer := valueInitializer(value, index)
					if mutableFacadeInitializerWithBindings(initializer, bindings, map[string]bool{}) {
						t.Errorf("%s declares mutable facade %s; use an owned function or immutable constructor dependency", path, identifier.Name)
					}
				}
			}
		}
	})
}

func TestCompositionPackagesDoNotDeclareMutableRegistries(t *testing.T) {
	assertCompositionStaticTypesDoNotExposeMutableRegistries(t)
	roots := []string{
		filepath.Join("..", "api"),
		filepath.Join("..", "gatewayinfrastructure"),
		filepath.Join("..", "gatewayworkspace"),
		filepath.Join("..", "..", "cmd", "aipermission"),
	}
	inspectProductionGoPackageRoots(t, roots, func(path string, file *ast.File, bindings map[string][]ast.Expr) {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, specification := range general.Specs {
				value := specification.(*ast.ValueSpec)
				for index, identifier := range value.Names {
					if identifier.Name == "_" {
						continue
					}
					if mutableRegistryType(value.Type, bindings, map[string]bool{}) || expressionConstructsMutableRegistry(valueInitializer(value, index), bindings, map[string]bool{}) {
						t.Errorf("%s declares mutable composition registry %s; keep owned state inside a component", path, identifier.Name)
					}
				}
			}
		}
	})
}

type typecheckPackage struct {
	ImportPath string
	Dir        string
	Export     string
	GoFiles    []string
	CgoFiles   []string
}

func assertCompositionStaticTypesDoNotExposeMutableRegistries(t *testing.T) {
	t.Helper()
	patterns := []string{"./internal/api/...", "./internal/gatewayinfrastructure/...", "./internal/gatewayworkspace/...", "./cmd/aipermission"}
	for _, buildContext := range supportedBackendBuildContexts {
		output := runGoList(t, buildContext, append([]string{"-deps", "-export", "-json"}, patterns...)...)
		packages, exports := decodeTypecheckPackages(t, output)
		for _, candidate := range packages {
			if !compositionPackagePath(candidate.ImportPath) {
				continue
			}
			for _, name := range mutablePackageVariables(t, candidate, exports) {
				t.Errorf("%s declares mutable composition registry %s; keep owned state inside a component", candidate.ImportPath, name)
			}
		}
	}
}

func decodeTypecheckPackages(t *testing.T, input []byte) ([]typecheckPackage, map[string]string) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(string(input)))
	packages := []typecheckPackage{}
	exports := map[string]string{}
	for {
		var candidate typecheckPackage
		if err := decoder.Decode(&candidate); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode typecheck package inventory: %v", err)
		}
		packages = append(packages, candidate)
		if candidate.Export != "" {
			exports[candidate.ImportPath] = candidate.Export
		}
	}
	return packages, exports
}

func mutablePackageVariables(t *testing.T, candidate typecheckPackage, exports map[string]string) []string {
	t.Helper()
	fileSet := token.NewFileSet()
	sourceFiles, err := compositionTypecheckFiles(candidate)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]*ast.File, 0, len(sourceFiles))
	for _, name := range sourceFiles {
		file, err := parser.ParseFile(fileSet, filepath.Join(candidate.Dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s for static registry analysis: %v", name, err)
		}
		files = append(files, file)
	}
	lookup := func(importPath string) (io.ReadCloser, error) {
		exportPath := exports[importPath]
		if exportPath == "" {
			return nil, fmt.Errorf("missing export data for %s", importPath)
		}
		return os.Open(exportPath)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	config := types.Config{Importer: importer.ForCompiler(fileSet, "gc", lookup)}
	if _, err := config.Check(candidate.ImportPath, fileSet, files, info); err != nil {
		t.Fatalf("type-check %s for static registry analysis: %v", candidate.ImportPath, err)
	}
	return mutableVariableNames(files, info)
}

func compositionTypecheckFiles(candidate typecheckPackage) ([]string, error) {
	if len(candidate.CgoFiles) > 0 {
		return nil, fmt.Errorf("composition package %s contains cgo sources that cannot be statically verified", candidate.ImportPath)
	}
	return candidate.GoFiles, nil
}

func mutableVariableNames(files []*ast.File, info *types.Info) []string {
	names := []string{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, specification := range general.Specs {
				value := specification.(*ast.ValueSpec)
				for index, identifier := range value.Names {
					object, ok := info.Defs[identifier].(*types.Var)
					if identifier.Name != "_" && ok && mutableStaticRegistryType(object.Type(), map[types.Type]bool{}) &&
						!verifiedErrorSentinel(object, valueInitializer(value, index), info) {
						names = append(names, identifier.Name)
					}
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

func verifiedErrorSentinel(variable *types.Var, initializer ast.Expr, info *types.Info) bool {
	if !types.Identical(types.Unalias(variable.Type()), types.Universe.Lookup("error").Type()) {
		return false
	}
	call, ok := initializer.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	function, ok := info.Uses[selector.Sel].(*types.Func)
	return ok && function.Pkg() != nil && function.Pkg().Path() == "errors" && function.Name() == "New"
}

func mutableStaticRegistryType(candidate types.Type, visiting map[types.Type]bool) bool {
	candidate = types.Unalias(candidate)
	if visiting[candidate] {
		return false
	}
	visiting[candidate] = true
	defer delete(visiting, candidate)
	switch typed := candidate.(type) {
	case *types.Basic:
		return typed.Kind() == types.UnsafePointer
	case *types.Map, *types.Slice, *types.Chan:
		return true
	case *types.Array:
		return typed.Len() > 0
	case *types.Interface:
		return true
	case *types.Signature:
		return true
	case *types.Pointer:
		return mutableStaticRegistryType(typed.Elem(), visiting)
	case *types.Struct:
		for index := 0; index < typed.NumFields(); index++ {
			if mutableStaticRegistryType(typed.Field(index).Type(), visiting) {
				return true
			}
		}
	case *types.Named:
		underlying := typed.Underlying()
		switch storage := underlying.(type) {
		case *types.Map, *types.Slice, *types.Chan:
			return true
		case *types.Array:
			return storage.Len() > 0
		}
		owner := typed.Obj().Pkg()
		if owner != nil && owner.Path() == "sync/atomic" && typed.Obj().Name() == "Pointer" && typed.TypeArgs().Len() == 1 {
			return mutableStaticRegistryType(typed.TypeArgs().At(0), visiting)
		}
		return mutableStaticRegistryType(underlying, visiting)
	}
	return false
}

func compositionPackagePath(packagePath string) bool {
	for _, root := range []string{modulePath + "/internal/api", modulePath + "/internal/gatewayinfrastructure", modulePath + "/internal/gatewayworkspace", modulePath + "/cmd/aipermission"} {
		if packagePath == root || strings.HasPrefix(packagePath, root+"/") {
			return true
		}
	}
	return false
}

func TestStaticRegistryTypesResolveFactoriesAndTuplePositions(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "fixture.go", `package fixture
import "maps"
import "sync/atomic"
import "errors"
import "reflect"
import "unsafe"
type factory struct{}
type Marker[T any] struct{}
type ArrayMarker[T any] [0]T
type mapError map[string]int
func (mapError) Error() string { return "" }
func (factory) NewMap() map[string]int { return nil }
func pair() (map[string]int, error) { return nil, nil }
func interfaceFactory() any { return map[string]int(nil) }
func errorFactory() error { return mapError(nil) }
var FromMethod = factory{}.NewMap()
var FromGeneric = maps.Clone(map[string]int{})
var FromIIFE = func() map[string]int { return nil }()
var FromAtomic atomic.Pointer[map[string]int]
var FromUnsafe unsafe.Pointer
var FromReflect reflect.Value
var FromFunction func()
var InnocentMarker Marker[map[string]int]
var InnocentArrayMarker ArrayMarker[map[string]int]
var FromInterface any = map[string]int(nil)
var FromInterfaceFactory = interfaceFactory()
var FromErrorFactory = errorFactory()
var InnocentError = errors.New("sentinel")
var FromFirst, _ = pair()
var _, FromPairError = pair()
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	config := types.Config{Importer: importer.Default()}
	if _, err := config.Check("example.invalid/fixture", fileSet, []*ast.File{file}, info); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(mutableVariableNames([]*ast.File{file}, info), ",")
	if got != "FromAtomic,FromErrorFactory,FromFirst,FromFunction,FromGeneric,FromIIFE,FromInterface,FromInterfaceFactory,FromMethod,FromPairError,FromReflect,FromUnsafe" {
		t.Fatalf("mutable variables = %q", got)
	}
}

func TestMutableStaticRegistryTypeInspectsExternalGenericStorage(t *testing.T) {
	external := types.NewPackage("example.invalid/external", "external")
	mapType := types.NewMap(types.Typ[types.String], types.Typ[types.Int])
	registry := types.NewNamed(
		types.NewTypeName(token.NoPos, external, "Registry", nil),
		types.NewStruct(
			[]*types.Var{types.NewField(token.NoPos, external, "entries", mapType, false)},
			[]string{""},
		),
		nil,
	)
	if !mutableStaticRegistryType(registry, map[types.Type]bool{}) {
		t.Fatal("external named struct containing a map was not classified as mutable")
	}
	empty := types.NewNamed(
		types.NewTypeName(token.NoPos, external, "Empty", nil),
		types.NewStruct(nil, nil),
		nil,
	)
	if mutableStaticRegistryType(empty, map[types.Type]bool{}) {
		t.Fatal("empty external named struct was classified as mutable")
	}

	holder := externalGenericType(external, "Holder", true)
	instantiatedHolder, err := types.Instantiate(nil, holder, []types.Type{mapType}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !mutableStaticRegistryType(instantiatedHolder, map[types.Type]bool{}) {
		t.Fatal("external generic storage containing a map was not classified as mutable")
	}

	marker := externalGenericType(external, "Marker", false)
	instantiatedMarker, err := types.Instantiate(nil, marker, []types.Type{mapType}, true)
	if err != nil {
		t.Fatal(err)
	}
	if mutableStaticRegistryType(instantiatedMarker, map[types.Type]bool{}) {
		t.Fatal("unused external generic type argument was classified as mutable storage")
	}
}

func externalGenericType(pkg *types.Package, name string, storesTypeParameter bool) *types.Named {
	typeParameter := types.NewTypeParam(
		types.NewTypeName(token.NoPos, pkg, "T", nil),
		types.Universe.Lookup("any").Type(),
	)
	underlying := types.Type(types.NewStruct(nil, nil))
	if storesTypeParameter {
		underlying = types.NewStruct(
			[]*types.Var{types.NewField(token.NoPos, pkg, "Value", typeParameter, false)},
			[]string{""},
		)
	}
	named := types.NewNamed(types.NewTypeName(token.NoPos, pkg, name, nil), underlying, nil)
	named.SetTypeParams([]*types.TypeParam{typeParameter})
	return named
}

func TestCompositionStaticTypesFailClosedForCgoSources(t *testing.T) {
	_, err := compositionTypecheckFiles(typecheckPackage{ImportPath: "example.invalid/composition", CgoFiles: []string{"registry.go"}})
	if err == nil || !strings.Contains(err.Error(), "cannot be statically verified") {
		t.Fatalf("cgo composition source error = %v", err)
	}
}

func packageFacadeBindings(file *ast.File) map[string][]ast.Expr {
	bindings := map[string][]ast.Expr{}
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv == nil {
			bindings[function.Name.Name] = append(bindings[function.Name.Name], &ast.FuncLit{})
			continue
		}
		general, ok := declaration.(*ast.GenDecl)
		if !ok || (general.Tok != token.VAR && general.Tok != token.CONST) {
			continue
		}
		var previousValues []ast.Expr
		var previousType ast.Expr
		for specificationIndex, specification := range general.Specs {
			value := specification.(*ast.ValueSpec)
			if len(value.Values) > 0 {
				previousValues = value.Values
				previousType = value.Type
			}
			effectiveType := value.Type
			if len(value.Values) == 0 {
				effectiveType = previousType
			}
			for index, identifier := range value.Names {
				initializer := valueInitializer(value, index)
				if initializer == nil && general.Tok == token.CONST {
					initializer = expressionAt(previousValues, index)
				}
				if initializer != nil {
					prefix := ""
					if general.Tok == token.CONST {
						prefix = "const:"
						bindings["const-iota:"+identifier.Name] = []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(specificationIndex)}}
						if effectiveType != nil {
							initializer = &ast.CallExpr{Fun: effectiveType, Args: []ast.Expr{initializer}}
						}
					}
					bindings[prefix+identifier.Name] = append(bindings[prefix+identifier.Name], initializer)
				}
			}
		}
	}
	return bindings
}

func expressionAt(expressions []ast.Expr, index int) ast.Expr {
	if index < len(expressions) {
		return expressions[index]
	}
	if len(expressions) == 1 {
		return expressions[0]
	}
	return nil
}

func bindingsWithIota(bindings map[string][]ast.Expr, constantName string) map[string][]ast.Expr {
	iotaValue := bindings["const-iota:"+constantName]
	if len(iotaValue) == 0 {
		return bindings
	}
	result := make(map[string][]ast.Expr, len(bindings)+1)
	for name, expressions := range bindings {
		result[name] = expressions
	}
	result["const:iota"] = iotaValue
	return result
}

func packageTypeBindings(file *ast.File) map[string][]ast.Expr {
	bindings := map[string][]ast.Expr{}
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec := specification.(*ast.TypeSpec)
			bindings[typeSpec.Name.Name] = append(bindings[typeSpec.Name.Name], typeSpec.Type)
		}
	}
	return bindings
}

func packageImportBindings(file *ast.File, importedTypes map[string]bool) map[string][]ast.Expr {
	bindings := map[string][]ast.Expr{}
	for _, specification := range file.Imports {
		packagePath, err := strconv.Unquote(specification.Path.Value)
		if err != nil {
			continue
		}
		alias := importedPackageName(packagePath, importedTypes)
		if specification.Name != nil {
			alias = specification.Name.Name
		}
		if alias == "_" {
			continue
		}
		key := "import:" + alias
		if alias == "." {
			key = "dot-import"
		}
		bindings[key] = append(bindings[key], &ast.BasicLit{
			Kind:  token.STRING,
			Value: strconv.Quote(packagePath),
		})
	}
	return bindings
}

func importedPackageName(packagePath string, importedTypes map[string]bool) string {
	for key := range importedTypes {
		parts := strings.SplitN(key, "\x00", 3)
		if len(parts) == 3 && parts[0] == packagePath {
			return parts[1]
		}
	}
	base := filepath.Base(packagePath)
	if token.IsIdentifier(base) && !versionedImportBase(base) {
		return base
	}
	if imported, err := importer.Default().Import(packagePath); err == nil {
		return imported.Name()
	}
	return base
}

func versionedImportBase(name string) bool {
	if len(name) < 2 || name[0] != 'v' {
		return false
	}
	for _, character := range name[1:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

type parsedProductionGoFile struct {
	path string
	file *ast.File
}

type scopedTypeDeclaration struct {
	name    string
	typeRef ast.Expr
	file    *ast.File
}

type scopedConstantDeclaration struct {
	packageKey string
	name       string
	value      ast.Expr
	file       *ast.File
}

const (
	constantExpressionInventoryPrefix = "const-expr:"
	constantTypeInventoryPrefix       = "constant-type:"
)

func productionPackageBindings(files []parsedProductionGoFile, importedMutableTypes map[string]bool) map[string][]ast.Expr {
	bindings := map[string][]ast.Expr{}
	declarations := []scopedTypeDeclaration{}
	constants := []scopedConstantDeclaration{}
	for _, parsed := range files {
		for name, initializers := range packageFacadeBindings(parsed.file) {
			bindings[name] = append(bindings[name], initializers...)
			if strings.HasPrefix(name, "const:") {
				for _, initializer := range initializers {
					constants = append(constants, scopedConstantDeclaration{
						name:  strings.TrimPrefix(name, "const:"),
						value: initializer,
						file:  parsed.file,
					})
				}
			}
		}
		for name, typeRefs := range packageTypeBindings(parsed.file) {
			for _, typeRef := range typeRefs {
				bindings["const-type:"+name] = append(bindings["const-type:"+name], typeRef)
				declarations = append(declarations, scopedTypeDeclaration{name: name, typeRef: typeRef, file: parsed.file})
			}
		}
	}
	resolvedConstants := map[int]bool{}
	for changed := true; changed; {
		changed = false
		for index, declaration := range constants {
			if resolvedConstants[index] {
				continue
			}
			scope := bindingsForFile(bindings, declaration.file, importedMutableTypes)
			scope = bindingsWithIota(scope, declaration.name)
			expanded, ok := expandConstantExpression(declaration.value, scope, map[string]bool{})
			if !ok {
				continue
			}
			if _, ok := standaloneConstantValue(expanded, scope); !ok {
				continue
			}
			bindings["const:"+declaration.name] = append(bindings["const:"+declaration.name], expanded)
			resolvedConstants[index] = true
			changed = true
		}
	}
	resolved := map[int]bool{}
	for changed := true; changed; {
		changed = false
		for index, declaration := range declarations {
			if resolved[index] {
				continue
			}
			scope := bindingsForFile(bindings, declaration.file, importedMutableTypes)
			if mutableRegistryType(declaration.typeRef, scope, map[string]bool{}) {
				bindings["type:"+declaration.name] = append(bindings["type:"+declaration.name], &ast.MapType{})
				resolved[index] = true
				changed = true
			}
		}
	}
	return bindings
}

func bindingsForFile(bindings map[string][]ast.Expr, file *ast.File, importedMutableTypes map[string]bool) map[string][]ast.Expr {
	imports := packageImportBindings(file, importedMutableTypes)
	result := make(map[string][]ast.Expr, len(bindings)+len(imports))
	for name, expressions := range bindings {
		result[name] = expressions
	}
	for name, expressions := range imports {
		result[name] = expressions
	}
	for _, specification := range file.Imports {
		packagePath, err := strconv.Unquote(specification.Path.Value)
		if err != nil {
			continue
		}
		for key := range importedMutableTypes {
			parts := strings.SplitN(key, "\x00", 3)
			if len(parts) != 3 || parts[0] != packagePath {
				continue
			}
			alias := parts[1]
			if specification.Name != nil {
				alias = specification.Name.Name
			}
			if strings.HasPrefix(parts[2], constantExpressionInventoryPrefix) {
				constantSpec := strings.TrimPrefix(parts[2], constantExpressionInventoryPrefix)
				constantName, encodedExpression, ok := strings.Cut(constantSpec, "=")
				if !ok {
					continue
				}
				source, err := base64.RawURLEncoding.DecodeString(encodedExpression)
				if err != nil {
					continue
				}
				constantExpression, err := parser.ParseExpr(string(source))
				if err != nil {
					continue
				}
				binding := "const:" + alias + "." + constantName
				if alias == "." {
					binding = "const:" + constantName
				}
				result[binding] = []ast.Expr{constantExpression}
				continue
			}
			if strings.HasPrefix(parts[2], constantTypeInventoryPrefix) {
				typeSpec := strings.TrimPrefix(parts[2], constantTypeInventoryPrefix)
				typeName, encodedExpression, ok := strings.Cut(typeSpec, "=")
				if !ok {
					continue
				}
				source, err := base64.RawURLEncoding.DecodeString(encodedExpression)
				if err != nil {
					continue
				}
				typeExpression, err := parser.ParseExpr(string(source))
				if err != nil {
					continue
				}
				binding := "const-type:" + alias + "." + typeName
				if alias == "." {
					binding = "const-type:" + typeName
				}
				result[binding] = []ast.Expr{typeExpression}
				continue
			}
			binding := "import-type:" + alias + "." + parts[2]
			if alias == "." {
				binding = "type:" + parts[2]
			}
			result[binding] = []ast.Expr{&ast.MapType{}}
		}
	}
	return result
}

func localMutableTypeInventory(t *testing.T, buildContext backendBuildContext) map[string]bool {
	t.Helper()
	return mutableTypeInventoryAt(t, filepath.Join("..", ".."), modulePath, buildContext)
}

func mutableTypeInventoryAt(t *testing.T, backendRoot, modulePrefix string, buildContext backendBuildContext) map[string]bool {
	t.Helper()
	type declaration struct {
		packagePath string
		packageName string
		name        string
		typeRef     ast.Expr
		file        *ast.File
	}
	declarations := []declaration{}
	constants := []scopedConstantDeclaration{}
	packageConstants := map[string]map[string][]ast.Expr{}
	for _, sourceRoot := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(backendRoot, sourceRoot), func(sourcePath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(sourcePath, ".go") || strings.HasSuffix(sourcePath, "_test.go") {
				return nil
			}
			matches, err := sourceMatchesBuildContext(sourcePath, buildContext)
			if err != nil {
				return err
			}
			if !matches {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), sourcePath, nil, 0)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(backendRoot, filepath.Dir(sourcePath))
			if err != nil {
				return err
			}
			packagePath := modulePrefix + "/" + filepath.ToSlash(relative)
			packageKey := packagePath + "\x00" + file.Name.Name
			if packageConstants[packageKey] == nil {
				packageConstants[packageKey] = map[string][]ast.Expr{}
			}
			for name, expressions := range packageFacadeBindings(file) {
				if strings.HasPrefix(name, "const:") || strings.HasPrefix(name, "const-iota:") {
					packageConstants[packageKey][name] = append(packageConstants[packageKey][name], expressions...)
				}
				if strings.HasPrefix(name, "const:") {
					constantName := strings.TrimPrefix(name, "const:")
					if !ast.IsExported(constantName) {
						continue
					}
					for _, expression := range expressions {
						constants = append(constants, scopedConstantDeclaration{
							packageKey: packageKey,
							name:       constantName,
							value:      expression,
							file:       file,
						})
					}
				}
			}
			for name, typeRefs := range packageTypeBindings(file) {
				for _, typeRef := range typeRefs {
					declarations = append(declarations, declaration{packagePath: packagePath, packageName: file.Name.Name, name: name, typeRef: typeRef, file: file})
					packageConstants[packageKey]["const-type:"+name] = append(packageConstants[packageKey]["const-type:"+name], typeRef)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("inventory local mutable types: %v", err)
		}
	}
	resolved := map[string]bool{}
	resolvedTypes := map[string]bool{}
	resolvedConstants := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, declaration := range declarations {
			if !ast.IsExported(declaration.name) {
				continue
			}
			typeKey := declaration.packagePath + "\x00" + declaration.packageName + "\x00" + declaration.name
			if resolvedTypes[typeKey] {
				continue
			}
			packageKey := declaration.packagePath + "\x00" + declaration.packageName
			bindings := bindingsForFile(packageConstants[packageKey], declaration.file, resolved)
			expanded, ok := expandConstantTypeExpression(declaration.typeRef, bindings, map[string]bool{})
			if !ok {
				continue
			}
			source, ok := formatConstantExpression(expanded)
			if !ok {
				continue
			}
			encoded := base64.RawURLEncoding.EncodeToString([]byte(source))
			resolved[packageKey+"\x00"+constantTypeInventoryPrefix+declaration.name+"="+encoded] = true
			resolvedTypes[typeKey] = true
			changed = true
		}
		for _, declaration := range constants {
			constantKey := declaration.packageKey + "\x00" + declaration.name
			if resolvedConstants[constantKey] {
				continue
			}
			bindings := bindingsForFile(packageConstants[declaration.packageKey], declaration.file, resolved)
			bindings = bindingsWithIota(bindings, declaration.name)
			expanded, ok := expandConstantExpression(
				declaration.value,
				bindings,
				map[string]bool{},
			)
			if !ok {
				continue
			}
			if _, ok := standaloneConstantValue(expanded, bindings); !ok {
				continue
			}
			source, ok := formatConstantExpression(expanded)
			if !ok {
				continue
			}
			encoded := base64.RawURLEncoding.EncodeToString([]byte(source))
			resolved[declaration.packageKey+"\x00"+constantExpressionInventoryPrefix+declaration.name+"="+encoded] = true
			resolvedConstants[constantKey] = true
			changed = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, declaration := range declarations {
			key := declaration.packagePath + "\x00" + declaration.packageName + "\x00" + declaration.name
			if resolved[key] {
				continue
			}
			packageKey := declaration.packagePath + "\x00" + declaration.packageName
			local := make(map[string][]ast.Expr, len(packageConstants[packageKey]))
			for name, expressions := range packageConstants[packageKey] {
				local[name] = expressions
			}
			for candidate := range resolved {
				parts := strings.SplitN(candidate, "\x00", 3)
				if len(parts) == 3 && parts[0] == declaration.packagePath && parts[1] == declaration.packageName &&
					!strings.HasPrefix(parts[2], constantExpressionInventoryPrefix) &&
					!strings.HasPrefix(parts[2], constantTypeInventoryPrefix) {
					local["type:"+parts[2]] = []ast.Expr{&ast.MapType{}}
				}
			}
			if mutableRegistryType(declaration.typeRef, bindingsForFile(local, declaration.file, resolved), map[string]bool{}) {
				resolved[key] = true
				changed = true
			}
		}
	}
	return resolved
}

func sourceMatchesBuildContext(sourcePath string, candidate backendBuildContext) (bool, error) {
	context := build.Default
	context.GOOS = candidate.goos
	context.GOARCH = candidate.goarch
	context.CgoEnabled = candidate.cgo == "1"
	context.BuildTags = append([]string(nil), candidate.tags...)
	matches, err := context.MatchFile(filepath.Dir(sourcePath), filepath.Base(sourcePath))
	if err != nil || !matches || context.CgoEnabled {
		return matches, err
	}
	file, err := parser.ParseFile(token.NewFileSet(), sourcePath, nil, parser.ImportsOnly)
	if err != nil {
		return false, err
	}
	for _, specification := range file.Imports {
		packagePath, err := strconv.Unquote(specification.Path.Value)
		if err != nil {
			return false, err
		}
		if packagePath == "C" {
			return false, nil
		}
	}
	return true, nil
}

func inspectProductionGoPackages(t *testing.T, root string, inspect func(string, *ast.File, map[string][]ast.Expr)) {
	t.Helper()
	inspectProductionGoPackageRoots(t, []string{root}, inspect)
}

func inspectProductionGoPackageRoots(t *testing.T, roots []string, inspect func(string, *ast.File, map[string][]ast.Expr)) {
	t.Helper()
	for _, buildContext := range supportedBackendBuildContexts {
		importedMutableTypes := localMutableTypeInventory(t, buildContext)
		for _, root := range roots {
			packages := map[string][]parsedProductionGoFile{}
			err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				matches, err := sourceMatchesBuildContext(path, buildContext)
				if err != nil {
					return err
				}
				if !matches {
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
				t.Fatalf("inspect production Go packages in %s for %s: %v", root, buildContext.name, err)
			}
			for _, files := range packages {
				bindings := productionPackageBindings(files, importedMutableTypes)
				for _, parsed := range files {
					inspect(parsed.path, parsed.file, bindingsForFile(bindings, parsed.file, importedMutableTypes))
				}
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

func TestProductionAPIDoesNotConsumeRawVaultRuntime(t *testing.T) {
	forbiddenPackage := modulePath + "/internal/gatewayvault"
	inspectProductionGoFiles(t, filepath.Join("..", "api"), func(path string, file *ast.File) {
		aliases, dotImports := importedPackageAliases(file, map[string]map[string]bool{
			forbiddenPackage: {"Runtime": true},
		})
		for _, packagePath := range dotImports {
			if packagePath == forbiddenPackage {
				t.Errorf("%s dot-imports %s; raw Vault runtime use must remain mechanically enforceable", path, packagePath)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if owner, ok := selector.X.(*ast.Ident); ok && aliases[owner.Name] == forbiddenPackage && selector.Sel.Name == "Runtime" {
				t.Errorf("%s consumes gatewayvault.Runtime; production API must use behavior-owned Vault applications", path)
			}
			if selector.Sel.Name == "VaultRuntime" {
				t.Errorf("%s calls VaultRuntime; production API must not project raw Vault state", path)
			}
			return true
		})
	})
}

func TestGatewayFeatureProjectionsCannotRecoverWorkspaceRuntime(t *testing.T) {
	root := filepath.Join("..", "gatewayinfrastructure")
	compositionFiles := map[string]bool{
		"component.go":      true,
		"infrastructure.go": true,
		"owners.go":         true,
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			packagePath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if packagePath == modulePath+"/internal/gatewayworkspace" {
				if !compositionFiles[name] {
					t.Errorf("%s imports gatewayworkspace; only explicit gateway composition files may retain workspace state", path)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect gateway feature projections: %v", err)
	}
}

func TestAPITransportDoesNotComposeConnectorExecutionInternals(t *testing.T) {
	forbiddenSelectors := map[string]bool{
		"TargetProfileByRuntimeID":          true,
		"NewCredentialBoundary":             true,
		"TransferRuntimeWithSecretAccessor": true,
		"FileTransferGateway":               true,
	}
	inspectProductionGoPackages(t, filepath.Join("..", "..", "internal", "api"), func(path string, file *ast.File, _ map[string][]ast.Expr) {
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.TypeSpec:
				if typed.Name.Name == "connectorSecretAccessor" {
					t.Errorf("%s defines connector secret access behavior; compose it in gatewayinfrastructure", path)
				}
			case *ast.SelectorExpr:
				if forbiddenSelectors[typed.Sel.Name] {
					t.Errorf("%s calls %s; API transport must receive an opaque connector operation", path, typed.Sel.Name)
				}
			}
			return true
		})
	})
}

func TestWorkspaceCapabilityProjectionIsBoundOnlyByGatewayInfrastructure(t *testing.T) {
	inspectProductionGoPackages(t, filepath.Join("..", "..", "internal"), func(path string, file *ast.File, _ map[string][]ast.Expr) {
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "ProjectCapabilities" {
				return true
			}
			if filepath.Clean(path) != filepath.Clean(filepath.Join("..", "..", "internal", "gatewayinfrastructure", "component.go")) {
				t.Errorf("%s binds workspace capability families; only gateway infrastructure composition may do so", path)
			}
			return true
		})
	})
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
		if composite, ok := value.X.(*ast.CompositeLit); ok && expressionsContainMutableFacade(composite.Elts, bindings, visiting) {
			return true
		}
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

func expressionConstructsMutableRegistry(expression ast.Expr, bindings map[string][]ast.Expr, visiting map[string]bool) bool {
	if expression == nil {
		return false
	}
	switch value := expression.(type) {
	case *ast.Ident:
		if visiting[value.Name] {
			return false
		}
		initializers := bindings[value.Name]
		visiting[value.Name] = true
		defer delete(visiting, value.Name)
		for _, initializer := range initializers {
			if expressionConstructsMutableRegistry(initializer, bindings, visiting) {
				return true
			}
		}
		return false
	case *ast.ParenExpr:
		return expressionConstructsMutableRegistry(value.X, bindings, visiting)
	case *ast.CompositeLit:
		return mutableRegistryType(value.Type, bindings, visiting)
	case *ast.CallExpr:
		if identifier, ok := value.Fun.(*ast.Ident); ok && (identifier.Name == "make" || identifier.Name == "new") {
			return len(value.Args) > 0 && mutableRegistryType(value.Args[0], bindings, visiting)
		}
		return mutableRegistryType(value.Fun, bindings, visiting)
	case *ast.UnaryExpr:
		return value.Op == token.AND && expressionConstructsMutableRegistry(value.X, bindings, visiting)
	default:
		return false
	}
}

func mutableRegistryType(expression ast.Expr, bindings map[string][]ast.Expr, visiting map[string]bool) bool {
	switch value := expression.(type) {
	case *ast.MapType, *ast.ChanType:
		return true
	case *ast.ArrayType:
		return arrayTypeHasStorage(value, bindings, visiting)
	case *ast.Ident:
		if value.Name == "Map" && bindingContainsPackage(bindings["dot-import"], "sync") {
			return true
		}
		key := "type:" + value.Name
		if visiting[key] {
			return false
		}
		visiting[key] = true
		defer delete(visiting, key)
		for _, declaration := range bindings[key] {
			if mutableRegistryType(declaration, bindings, visiting) {
				return true
			}
		}
		return false
	case *ast.ParenExpr:
		return mutableRegistryType(value.X, bindings, visiting)
	case *ast.StarExpr:
		return mutableRegistryType(value.X, bindings, visiting)
	case *ast.IndexExpr:
		return mutableRegistryType(value.X, bindings, visiting)
	case *ast.IndexListExpr:
		return mutableRegistryType(value.X, bindings, visiting)
	case *ast.StructType:
		for _, field := range value.Fields.List {
			if mutableRegistryType(field.Type, bindings, visiting) {
				return true
			}
		}
		return false
	case *ast.SelectorExpr:
		owner, ok := value.X.(*ast.Ident)
		return ok && ((value.Sel.Name == "Map" &&
			(owner.Name == "sync" || bindingContainsPackage(bindings["import:"+owner.Name], "sync"))) ||
			len(bindings["import-type:"+owner.Name+"."+value.Sel.Name]) > 0)
	default:
		return false
	}
}

func arrayTypeHasStorage(candidate *ast.ArrayType, bindings map[string][]ast.Expr, visiting map[string]bool) bool {
	if candidate.Len == nil {
		return true
	}
	length, ok := integerConstantValue(candidate.Len, bindings, visiting)
	return !ok || constant.Sign(length) != 0
}

func bindingContainsPackage(bindings []ast.Expr, expected string) bool {
	for _, binding := range bindings {
		literal, ok := binding.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			continue
		}
		value, err := strconv.Unquote(literal.Value)
		if err == nil && value == expected {
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
		"private":  {mustParseExpression(t, "owner.Function")},
	}
	if !mutableFacadeInitializerWithBindings(bindings["exported"][0], bindings, map[string]bool{}) {
		t.Fatal("identifier-chain mutable facade escaped detection")
	}
	if !mutableFacadeInitializerWithBindings(bindings["private"][0], bindings, map[string]bool{}) {
		t.Fatal("unexported mutable facade escaped detection")
	}
	if !mutableFacadeInitializerWithBindings(mustParseExpression(t, "struct{ Run func() }{Run: owner.Function}"), nil, map[string]bool{}) {
		t.Fatal("function-bearing package value escaped mutable facade detection")
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
	functionFile, err := parser.ParseFile(token.NewFileSet(), "factory_linux.go", `//go:build linux
package fixture
func NewLockedServer() {}
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	variableFile, err := parser.ParseFile(token.NewFileSet(), "facade.go", `package fixture
var UnsafeFactory = NewLockedServer
var ConstructedValue = NewLockedServer()
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	functionBindings := packageFacadeBindings(functionFile)
	for name, initializers := range packageFacadeBindings(variableFile) {
		functionBindings[name] = append(functionBindings[name], initializers...)
	}
	if !mutableFacadeInitializerWithBindings(functionBindings["UnsafeFactory"][0], functionBindings, map[string]bool{}) {
		t.Fatal("package function reference escaped mutable facade detection")
	}
	if mutableFacadeInitializerWithBindings(functionBindings["ConstructedValue"][0], functionBindings, map[string]bool{}) {
		t.Fatal("package function call result was mistaken for a mutable facade")
	}
	for _, source := range []string{
		`map[string]any{}`,
		`make(map[string]any)`,
		`[]any{}`,
		`make(chan any)`,
		`sync.Map{}`,
		`new(sync.Map)`,
	} {
		if !expressionConstructsMutableRegistry(mustParseExpression(t, source), nil, map[string]bool{}) {
			t.Errorf("mutable registry %q escaped detection", source)
		}
	}
	registryBindings := map[string][]ast.Expr{
		"privateRegistry": {mustParseExpression(t, `map[string]any{}`)},
		"PublicRegistry":  {mustParseExpression(t, "privateRegistry")},
		"type:registry":   {mustParseExpression(t, `map[string]any`)},
	}
	if !expressionConstructsMutableRegistry(registryBindings["PublicRegistry"][0], registryBindings, map[string]bool{}) {
		t.Fatal("identifier-chain mutable registry escaped detection")
	}
	if !expressionConstructsMutableRegistry(mustParseExpression(t, "registry{}"), registryBindings, map[string]bool{}) {
		t.Fatal("named mutable registry type escaped detection")
	}
	if !expressionConstructsMutableRegistry(mustParseExpression(t, "registry(nil)"), registryBindings, map[string]bool{}) {
		t.Fatal("mutable registry type conversion escaped detection")
	}
	assertAliasedMutableRegistriesDetected(t)
}

func supportedBuildContext(t *testing.T, name string) backendBuildContext {
	t.Helper()
	for _, candidate := range supportedBackendBuildContexts {
		if candidate.name == name {
			return candidate
		}
	}
	t.Fatalf("supported build context %q not found", name)
	return backendBuildContext{}
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
