package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceMatchesBuildContextExcludesCgoImportsWhenDisabled(t *testing.T) {
	root := t.TempDir()
	regularPath := filepath.Join(root, "registry_windows.go")
	if err := os.WriteFile(regularPath, []byte("package fixture\nvar regular = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cgoPath := filepath.Join(root, "registry_cgo_windows.go")
	if err := os.WriteFile(cgoPath, []byte("package fixture\nimport \"C\"\nvar cgoOnly = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	windowsContext := supportedBuildContext(t, "windows")
	matches, err := sourceMatchesBuildContext(regularPath, windowsContext)
	if err != nil || !matches {
		t.Fatalf("regular Windows source match = %t, %v", matches, err)
	}
	matches, err = sourceMatchesBuildContext(cgoPath, windowsContext)
	if err != nil {
		t.Fatal(err)
	}
	if matches {
		t.Fatal("CGO source matched a build context with CGO disabled")
	}
}

func assertAliasedMutableRegistriesDetected(t *testing.T) {
	t.Helper()
	linuxContext := supportedBuildContext(t, "linux")
	windowsContext := supportedBuildContext(t, "windows")
	platformFile, err := parser.ParseFile(token.NewFileSet(), "registry_windows.go", `//go:build windows
package fixture
import (
	sm "sync"
	"time"
)
	type registry = sm.Map
	type genericRegistry[T any] map[string]T
	type keyedRegistry[K comparable, V any] map[K]V
	type wrappedRegistry struct { items map[string]any }
	type Count uint8
	type GenericCount[T any] uint8
	type uint16 int16
	type Empty [zero]int
	const zero = 1 - 1
	const zeroIota = iota
	const zeroChar = '\x00'
	const zeroFloat = 0.0
	const zeroLength = len("")
	const zeroConverted = uint8(0)
	const zeroComplement = ^uint8(255)
	const zeroNamed = Count(0)
	const zeroGeneric = GenericCount[string](0)
	const zeroDuration = time.Duration(0)
	const zeroExternal = time.Second - time.Second
	const zeroComposite = len([zero]int{})
	const zeroShadowed = ^uint16(-1)
	const zeroNamedArray = len(Empty{})
	const typedMax uint8 = 255
	const zeroTypedComplement = ^typedMax
	type zeroArrayRegistry[T any] [zero]T
	var Direct sm.Map
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	consumerFile, err := parser.ParseFile(token.NewFileSet(), "registry.go", `package fixture
var ThroughAlias registry
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	dotFile, err := parser.ParseFile(token.NewFileSet(), "dot.go", `package fixture
import . "sync"
var ThroughDotImport Map
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	files := []parsedProductionGoFile{{file: platformFile}, {file: consumerFile}, {file: dotFile}}
	bindings := productionPackageBindings(files, nil)
	assertConstantTypeResolution(t, bindings, platformFile)
	fixtures := []struct {
		expression string
		file       *ast.File
	}{
		{"sm.Map", platformFile}, {"registry", consumerFile}, {"Map", dotFile},
		{"genericRegistry[any]", consumerFile}, {"keyedRegistry[string, any]", consumerFile},
		{"wrappedRegistry", consumerFile},
		{"struct{ items map[string]any }", consumerFile},
	}
	for _, fixture := range fixtures {
		expression := fixture.expression
		scope := bindingsForFile(bindings, fixture.file, nil)
		if !mutableRegistryType(mustParseExpression(t, expression), scope, map[string]bool{}) {
			t.Errorf("aliased mutable registry %q escaped detection", expression)
		}
	}

	otherFile, err := parser.ParseFile(token.NewFileSet(), "other.go", `package fixture
import sm "example.invalid/not-sync"
var Innocent sm.Map
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	otherScope := bindingsForFile(bindings, otherFile, nil)
	if mutableRegistryType(mustParseExpression(t, "sm.Map"), otherScope, map[string]bool{}) {
		t.Fatal("file-scoped import alias was contaminated by another file")
	}
	for _, expression := range []string{
		"[0]map[string]any", "[1-1]map[string]any", "[1/2]map[string]any", "[zeroIota]map[string]any",
		"[zeroChar]map[string]any", "[zeroFloat]map[string]any", "[zeroLength]map[string]any",
		"[zeroConverted]map[string]any", "[zeroComplement]map[string]any",
		"[zeroNamed]map[string]any", "[zeroGeneric]map[string]any", "[zeroDuration]map[string]any",
		"[zeroExternal]map[string]any", "[zeroComposite]map[string]any", "[zeroShadowed]map[string]any",
		"[zeroNamedArray]map[string]any",
		"[zeroTypedComplement]map[string]any",
		"zeroArrayRegistry[map[string]any]",
	} {
		if mutableRegistryType(mustParseExpression(t, expression), bindingsForFile(bindings, consumerFile, nil), map[string]bool{}) {
			t.Fatalf("zero-length array %q was classified as mutable storage", expression)
		}
	}

	localTypes := localMutableTypeInventory(t, linuxContext)
	importedFile, err := parser.ParseFile(token.NewFileSet(), "imported.go", `package fixture
import transport "github.com/aipermission/aipermission/backend/internal/connectortransport"
var Shared transport.Capabilities
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	importedScope := bindingsForFile(nil, importedFile, localTypes)
	if !mutableRegistryType(mustParseExpression(t, "transport.Capabilities"), importedScope, map[string]bool{}) {
		t.Fatal("imported named mutable registry escaped detection")
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "registryv2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "registryv2", "registry.go"), []byte("package registry\ntype Table[K comparable, V any] map[K]V\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mismatchedTypes := mutableTypeInventoryAt(t, root, "example.invalid/project", linuxContext)
	mismatchedImport, err := parser.ParseFile(token.NewFileSet(), "consumer.go", `package fixture
import "example.invalid/project/internal/registryv2"
var Shared registry.Table[string, any]
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	mismatchedScope := bindingsForFile(nil, mismatchedImport, mismatchedTypes)
	if !mutableRegistryType(mustParseExpression(t, "registry.Table[string, any]"), mismatchedScope, map[string]bool{}) {
		t.Fatal("declared package name mismatch escaped imported mutable registry detection")
	}

	splitRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(splitRoot, "internal", "split"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(splitRoot, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(splitRoot, "internal", "split", name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture("shared_linux.go", "//go:build linux\npackage split\ntype Shared map[string]any\n")
	writeFixture("shared_windows.go", "//go:build windows\npackage split\ntype Shared struct{}\ntype Wrapper Shared\n")
	linuxInventory := mutableTypeInventoryAt(t, splitRoot, "example.invalid/project", linuxContext)
	if !linuxInventory["example.invalid/project/internal/split\x00split\x00Shared"] {
		t.Fatal("mutable build-tag package type escaped inventory")
	}
	windowsInventory := mutableTypeInventoryAt(t, splitRoot, "example.invalid/project", windowsContext)
	if windowsInventory["example.invalid/project/internal/split\x00split\x00Shared"] ||
		windowsInventory["example.invalid/project/internal/split\x00split\x00Wrapper"] {
		t.Fatal("mutually exclusive build-tag types contaminated mutable type inventory")
	}

	markerRoot := t.TempDir()
	for _, packageName := range []string{"marker", "limits", "aliases"} {
		if err := os.MkdirAll(filepath.Join(markerRoot, "internal", packageName), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(markerRoot, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	limitsSource := "package limits\ntype Count uint8\nconst Base Count = 0\nconst Zero = iota\nconst Prefix = \"\"\n"
	if err := os.WriteFile(filepath.Join(markerRoot, "internal", "limits", "limits.go"), []byte(limitsSource), 0o600); err != nil {
		t.Fatal(err)
	}
	aliasesSource := "package aliases\nimport \"example.invalid/project/internal/limits\"\nconst Zero = limits.Count(limits.Base)\nconst IotaZero = limits.Zero\nconst EmptyLength = len(limits.Prefix)\n"
	if err := os.WriteFile(filepath.Join(markerRoot, "internal", "aliases", "aliases.go"), []byte(aliasesSource), 0o600); err != nil {
		t.Fatal(err)
	}
	markerSource := "package marker\nimport \"example.invalid/project/internal/aliases\"\ntype Empty [aliases.Zero]int\ntype Marker[T any] [aliases.Zero]T\ntype IotaMarker[T any] [aliases.IotaZero]T\ntype TextMarker[T any] [aliases.EmptyLength]T\ntype ArrayMarker[T any] [len(Empty{})]T\n"
	if err := os.WriteFile(filepath.Join(markerRoot, "internal", "marker", "marker.go"), []byte(markerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	markerInventory := mutableTypeInventoryAt(t, markerRoot, "example.invalid/project", linuxContext)
	markerImport, err := parser.ParseFile(token.NewFileSet(), "marker_consumer.go", `package fixture
import "example.invalid/project/internal/marker"
var Innocent marker.Marker[map[string]any]
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	markerScope := bindingsForFile(nil, markerImport, markerInventory)
	if mutableRegistryType(mustParseExpression(t, "marker.Marker[map[string]any]"), markerScope, map[string]bool{}) {
		t.Fatal("imported constant-zero marker was classified as mutable storage")
	}
	if mutableRegistryType(mustParseExpression(t, "marker.TextMarker[map[string]any]"), markerScope, map[string]bool{}) {
		t.Fatal("imported len-based marker was classified as mutable storage")
	}
	if mutableRegistryType(mustParseExpression(t, "marker.IotaMarker[map[string]any]"), markerScope, map[string]bool{}) {
		t.Fatal("imported iota-based marker was classified as mutable storage")
	}
	if mutableRegistryType(mustParseExpression(t, "marker.ArrayMarker[map[string]any]"), markerScope, map[string]bool{}) {
		t.Fatal("imported named-array marker was classified as mutable storage")
	}
}

func assertConstantTypeResolution(t *testing.T, bindings map[string][]ast.Expr, platformFile *ast.File) {
	t.Helper()
	if converted, ok := expandConstantTypeExpression(mustParseExpression(t, "time.Duration"), bindingsForFile(bindings, platformFile, nil), map[string]bool{}); !ok {
		t.Fatal("external named scalar conversion type escaped resolution")
	} else if source, _ := formatConstantExpression(converted); source != "int64" {
		t.Fatalf("external named scalar conversion type = %q", source)
	}
	resolvedDuration := false
	for _, initializer := range bindings["const:zeroDuration"] {
		if source, _ := formatConstantExpression(initializer); source == "int64(0)" {
			resolvedDuration = true
		}
	}
	if !resolvedDuration {
		t.Fatal("external named scalar constant was not normalized in its declaration scope")
	}
	versionedFile, err := parser.ParseFile(token.NewFileSet(), "versioned.go", `package fixture
import "example.invalid/library/v2"
var _ = library.Zero
`, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	versionedTypes := map[string]bool{"example.invalid/library/v2\x00library\x00constant-type:Count=dWludDg": true}
	if len(packageImportBindings(versionedFile, versionedTypes)["import:library"]) != 1 {
		t.Fatal("versioned import path basename replaced its declared package name")
	}
	shadowFile, err := parser.ParseFile(token.NewFileSet(), "shadow.go", `package fixture
type uint8 int16
const Different = ^uint8(255)
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	shadowBindings := productionPackageBindings([]parsedProductionGoFile{{file: shadowFile}}, nil)
	if !mutableRegistryType(mustParseExpression(t, "[Different]map[string]any"), shadowBindings, map[string]bool{}) {
		t.Fatal("locally shadowed predeclared conversion type used universe semantics")
	}
}
