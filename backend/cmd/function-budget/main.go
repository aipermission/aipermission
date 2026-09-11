package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultMaxLines          = 180
	defaultMaxComplexity     = 35
	defaultMaxTestLines      = 220
	defaultMaxTestComplexity = 60
)

type finding struct {
	path       string
	function   string
	metric     string
	actual     int
	configured int
}

func main() {
	findings, err := inspectTree("internal")
	if err != nil {
		fmt.Fprintf(os.Stderr, "function budget check failed: %v\n", err)
		os.Exit(1)
	}
	if len(findings) > 0 {
		fmt.Fprintln(os.Stderr, "Go function budget check failed:")
		for _, item := range findings {
			fmt.Fprintf(os.Stderr, "- %s:%s has %s %d; budget is %d\n", item.path, item.function, item.metric, item.actual, item.configured)
		}
		os.Exit(1)
	}
	fmt.Println("Go function budgets passed.")
}

func inspectTree(root string) ([]finding, error) {
	fileSet := token.NewFileSet()
	findings := []finding{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		maxLines, maxComplexity := defaultMaxLines, defaultMaxComplexity
		if strings.HasSuffix(path, "_test.go") {
			maxLines, maxComplexity = defaultMaxTestLines, defaultMaxTestComplexity
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			inspectFunction(fileSet, filepath.ToSlash(path), functionName(function), function, function.Body, maxLines, maxComplexity, &findings)
		}
		return nil
	})
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i].path + findings[i].function + findings[i].metric
		right := findings[j].path + findings[j].function + findings[j].metric
		return left < right
	})
	return findings, err
}

func inspectFunction(fileSet *token.FileSet, path string, name string, node ast.Node, body *ast.BlockStmt, maxLines int, maxComplexity int, findings *[]finding) {
	lines := fileSet.Position(node.End()).Line - fileSet.Position(node.Pos()).Line + 1
	complexity := cyclomaticComplexity(body)
	if lines > maxLines {
		*findings = append(*findings, finding{path: path, function: name, metric: "lines", actual: lines, configured: maxLines})
	}
	if complexity > maxComplexity {
		*findings = append(*findings, finding{path: path, function: name, metric: "complexity", actual: complexity, configured: maxComplexity})
	}

	closures := []*ast.FuncLit{}
	ast.Inspect(body, func(child ast.Node) bool {
		closure, ok := child.(*ast.FuncLit)
		if !ok {
			return true
		}
		closures = append(closures, closure)
		return false
	})
	for index, closure := range closures {
		inspectFunction(fileSet, path, fmt.Sprintf("%s$closure%d", name, index+1), closure, closure.Body, maxLines, maxComplexity, findings)
	}
}

func functionName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return function.Name.Name
	}
	receiver := function.Recv.List[0].Type
	if pointer, ok := receiver.(*ast.StarExpr); ok {
		receiver = pointer.X
	}
	if identifier, ok := receiver.(*ast.Ident); ok {
		return identifier.Name + "." + function.Name.Name
	}
	return function.Name.Name
}

func cyclomaticComplexity(body *ast.BlockStmt) int {
	complexity := 1
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		switch value := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			complexity++
		case *ast.CaseClause:
			if len(value.List) > 0 {
				complexity++
			}
		case *ast.CommClause:
			if value.Comm != nil {
				complexity++
			}
		case *ast.BinaryExpr:
			if value.Op == token.LAND || value.Op == token.LOR {
				complexity++
			}
		}
		return true
	})
	return complexity
}
