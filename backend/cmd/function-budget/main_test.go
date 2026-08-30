package main

import (
	"go/ast"
	"go/parser"
	"go/token"
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
