package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCyclomaticComplexityExcludesNestedClosures(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package fixture
		func outer(value bool) {
			if value {}
			callback := func(first bool, second bool) {
				if first && second {}
			}
			_ = callback
		}`, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	function := file.Decls[0].(*ast.FuncDecl)
	if got := cyclomaticComplexity(function.Body); got != 2 {
		t.Fatalf("outer complexity = %d, want 2", got)
	}
	var closure *ast.FuncLit
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if value, ok := node.(*ast.FuncLit); ok {
			closure = value
			return false
		}
		return true
	})
	if got := cyclomaticComplexity(closure.Body); got != 3 {
		t.Fatalf("closure complexity = %d, want 3", got)
	}
}

func TestInspectTreeAppliesSeparateTestFunctionBudget(t *testing.T) {
	root := t.TempDir()
	source := "package fixture\nfunc oversized() {\n" + strings.Repeat("\n", defaultMaxLines) + "}\n"
	for _, name := range []string{"production.go", "fixture_test.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	findings, err := inspectTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || !strings.HasSuffix(findings[0].path, "production.go") || findings[0].metric != "lines" {
		t.Fatalf("findings = %#v, want only production line-budget violation", findings)
	}
}
