package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func forbiddenConnectorKindComparison(t *testing.T, source string) bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "boundary.go", source, 0)
	if err != nil {
		t.Fatalf("parse connector boundary: %v", err)
	}
	forbidden := false
	ast.Inspect(file, func(node ast.Node) bool {
		if dispatch, ok := node.(*ast.SwitchStmt); ok && forbiddenConnectorKindSwitch(dispatch) {
			forbidden = true
		}
		comparison, ok := node.(*ast.BinaryExpr)
		if !ok || (comparison.Op != token.EQL && comparison.Op != token.NEQ) {
			return true
		}
		left, right := comparison.X, comparison.Y
		if !connectorKindField(left) {
			left, right = right, left
		}
		if connectorKindField(left) && !connectorKindField(right) && !emptyStringExpression(right) {
			forbidden = true
		}
		return true
	})
	return forbidden
}

func forbiddenConnectorKindSwitch(dispatch *ast.SwitchStmt) bool {
	if !connectorKindField(dispatch.Tag) {
		return false
	}
	for _, statement := range dispatch.Body.List {
		branch, ok := statement.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, expression := range branch.List {
			if !emptyStringExpression(expression) {
				return true
			}
		}
	}
	return false
}

func connectorKindField(expression ast.Expr) bool {
	if parentheses, ok := expression.(*ast.ParenExpr); ok {
		return connectorKindField(parentheses.X)
	}
	field, ok := expression.(*ast.SelectorExpr)
	return ok && field.Sel.Name == "ConnectorKind"
}

func emptyStringExpression(expression ast.Expr) bool {
	if parentheses, ok := expression.(*ast.ParenExpr); ok {
		return emptyStringExpression(parentheses.X)
	}
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(literal.Value)
	return err == nil && value == ""
}

func TestConnectorKindComparisonBoundary(t *testing.T) {
	for _, example := range []struct {
		name       string
		expression string
		forbidden  bool
	}{
		{"missing kind", `target.ConnectorKind == ""`, false},
		{"nonempty kind", `target.ConnectorKind != ""`, false},
		{"reversed empty", `"" == target.ConnectorKind`, false},
		{"identity match", `profile.ConnectorKind == target.ConnectorKind`, false},
		{"identity mismatch", `profile.ConnectorKind != target.ConnectorKind`, false},
		{"parenthesized identity", `(profile.ConnectorKind) != (target.ConnectorKind)`, false},
		{"unrelated field", `profile.Kind == "username_password"`, false},
		{"kind equality", `target.ConnectorKind == "ssh"`, true},
		{"kind inequality", `target.ConnectorKind != "postgres"`, true},
		{"reversed kind", `"redis" == target.ConnectorKind`, true},
		{"escaped kind", `target.ConnectorKind == "\x73sh"`, true},
		{"parenthesized kind", `(target.ConnectorKind) == ("s3")`, true},
		{"raw kind", "target.ConnectorKind == `docker`", true},
		{"constant alias", `target.ConnectorKind == namedKind`, true},
		{"runtime alias", `target.ConnectorKind != selectedKind`, true},
	} {
		t.Run(example.name, func(t *testing.T) {
			source := "package boundary\nfunc check() bool { return " + example.expression + " }"
			if actual := forbiddenConnectorKindComparison(t, source); actual != example.forbidden {
				t.Fatalf("comparison %s forbidden=%v, want %v", example.expression, actual, example.forbidden)
			}
		})
	}
}

func TestConnectorKindSwitchBoundary(t *testing.T) {
	for _, example := range []struct {
		body      string
		forbidden bool
	}{
		{`switch target.ConnectorKind { case "": return; default: return }`, false},
		{`switch target.ConnectorKind { case "ssh": return }`, true},
		{`switch (target.ConnectorKind) { case "", "postgres": return }`, true},
		{`switch target.ConnectorKind { case namedKind: return }`, true},
		{`switch profile.Kind { case "username_password": return }`, false},
	} {
		if actual := forbiddenConnectorKindComparison(t, "package boundary\nfunc check() {"+example.body+"}"); actual != example.forbidden {
			t.Fatalf("switch %s forbidden=%v, want %v", example.body, actual, example.forbidden)
		}
	}
}
