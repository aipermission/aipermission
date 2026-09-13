package architecture

import (
	"bytes"
	"go/ast"
	"go/constant"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
)

func integerConstantValue(expression ast.Expr, bindings map[string][]ast.Expr, visiting map[string]bool) (constant.Value, bool) {
	expanded, ok := expandConstantExpression(expression, bindings, visiting)
	if !ok {
		return nil, false
	}
	value, ok := standaloneConstantValue(expanded, bindings)
	if !ok {
		return nil, false
	}
	value = constant.ToInt(value)
	return value, value.Kind() == constant.Int
}

func expandConstantExpression(expression ast.Expr, bindings map[string][]ast.Expr, visiting map[string]bool) (ast.Expr, bool) {
	if !expressionRequiresScopedResolution(expression, bindings) {
		if _, ok := standaloneConstantValue(expression, bindings); ok {
			return expression, true
		}
	}
	switch value := expression.(type) {
	case *ast.BasicLit:
		return value, true
	case *ast.ParenExpr:
		expanded, ok := expandConstantExpression(value.X, bindings, visiting)
		return &ast.ParenExpr{X: expanded}, ok
	case *ast.Ident:
		key := "const:" + value.Name
		if visiting[key] {
			return nil, false
		}
		if len(bindings[key]) == 0 {
			return nil, false
		}
		visiting[key] = true
		defer delete(visiting, key)
		for _, initializer := range bindings[key] {
			scopedBindings := bindingsWithIota(bindings, value.Name)
			if expanded, ok := expandConstantExpression(initializer, scopedBindings, visiting); ok {
				if _, valid := standaloneConstantValue(expanded, scopedBindings); valid {
					return expanded, true
				}
			}
		}
		return nil, false
	case *ast.SelectorExpr:
		owner, ok := value.X.(*ast.Ident)
		if !ok {
			return nil, false
		}
		key := "const:" + owner.Name + "." + value.Sel.Name
		if visiting[key] {
			return nil, false
		}
		visiting[key] = true
		defer delete(visiting, key)
		for _, initializer := range bindings[key] {
			if expanded, ok := expandConstantExpression(initializer, bindings, visiting); ok {
				if _, valid := standaloneConstantValue(expanded, bindings); valid {
					return expanded, true
				}
			}
		}
		return importedConstantExpression(owner.Name, value.Sel.Name, bindings)
	case *ast.UnaryExpr:
		expanded, ok := expandConstantExpression(value.X, bindings, visiting)
		return &ast.UnaryExpr{Op: value.Op, X: expanded}, ok
	case *ast.BinaryExpr:
		left, leftOK := expandConstantExpression(value.X, bindings, visiting)
		right, rightOK := expandConstantExpression(value.Y, bindings, visiting)
		return &ast.BinaryExpr{X: left, Op: value.Op, Y: right}, leftOK && rightOK
	case *ast.CallExpr:
		arguments := make([]ast.Expr, len(value.Args))
		for index, argument := range value.Args {
			expanded, ok := expandConstantExpression(argument, bindings, visiting)
			if !ok {
				return nil, false
			}
			arguments[index] = expanded
		}
		function := value.Fun
		if convertedType, ok := expandConstantTypeExpression(value.Fun, bindings, visiting); ok {
			function = convertedType
		}
		return &ast.CallExpr{Fun: function, Args: arguments}, true
	case *ast.CompositeLit:
		typeExpression, ok := expandConstantTypeExpression(value.Type, bindings, visiting)
		arrayType, arrayOK := typeExpression.(*ast.ArrayType)
		if !ok || !arrayOK || arrayType.Len == nil {
			return nil, false
		}
		return &ast.CompositeLit{Type: arrayType}, true
	default:
		return nil, false
	}
}

func expressionRequiresScopedResolution(expression ast.Expr, bindings map[string][]ast.Expr) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if _, ok := node.(*ast.SelectorExpr); ok {
			found = true
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if ok {
			if identifier, ok := call.Fun.(*ast.Ident); ok && len(bindings["const-type:"+identifier.Name]) > 0 {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

func expandConstantTypeExpression(expression ast.Expr, bindings map[string][]ast.Expr, visiting map[string]bool) (ast.Expr, bool) {
	switch value := expression.(type) {
	case *ast.Ident:
		key := "const-type:" + value.Name
		if visiting[key] {
			return nil, false
		}
		if len(bindings[key]) > 0 {
			visiting[key] = true
			defer delete(visiting, key)
			for _, underlying := range bindings[key] {
				if expanded, ok := expandConstantTypeExpression(underlying, bindings, visiting); ok {
					return expanded, true
				}
			}
			return nil, false
		}
		if object, ok := types.Universe.Lookup(value.Name).(*types.TypeName); ok && object != nil {
			return value, true
		}
		return nil, false
	case *ast.ParenExpr:
		expanded, ok := expandConstantTypeExpression(value.X, bindings, visiting)
		return &ast.ParenExpr{X: expanded}, ok
	case *ast.IndexExpr:
		return expandConstantTypeExpression(value.X, bindings, visiting)
	case *ast.IndexListExpr:
		return expandConstantTypeExpression(value.X, bindings, visiting)
	case *ast.ArrayType:
		if value.Len == nil {
			return nil, false
		}
		length, ok := expandConstantExpression(value.Len, bindings, visiting)
		if !ok {
			return nil, false
		}
		return &ast.ArrayType{Len: length, Elt: &ast.StructType{Fields: &ast.FieldList{}}}, true
	case *ast.SelectorExpr:
		owner, ok := value.X.(*ast.Ident)
		if !ok {
			return nil, false
		}
		key := "const-type:" + owner.Name + "." + value.Sel.Name
		if visiting[key] {
			return nil, false
		}
		visiting[key] = true
		defer delete(visiting, key)
		for _, underlying := range bindings[key] {
			if expanded, ok := expandConstantTypeExpression(underlying, bindings, visiting); ok {
				return expanded, true
			}
		}
		return importedConstantConversionType(owner.Name, value.Sel.Name, bindings)
	default:
		return nil, false
	}
}

func importedConstantConversionType(alias, typeName string, bindings map[string][]ast.Expr) (ast.Expr, bool) {
	object, ok := importedPackageObject(alias, typeName, bindings).(*types.TypeName)
	if !ok {
		return nil, false
	}
	basic, ok := object.Type().Underlying().(*types.Basic)
	if !ok || basic.Info()&types.IsConstType == 0 {
		return nil, false
	}
	return ast.NewIdent(basic.Name()), true
}

func importedConstantExpression(alias, constantName string, bindings map[string][]ast.Expr) (ast.Expr, bool) {
	object, ok := importedPackageObject(alias, constantName, bindings).(*types.Const)
	if !ok {
		return nil, false
	}
	literal, err := parser.ParseExpr(object.Val().ExactString())
	if err != nil {
		return nil, false
	}
	basic, ok := object.Type().Underlying().(*types.Basic)
	if !ok || basic.Info()&types.IsConstType == 0 {
		return nil, false
	}
	if basic.Info()&types.IsUntyped != 0 {
		return literal, true
	}
	return &ast.CallExpr{Fun: ast.NewIdent(basic.Name()), Args: []ast.Expr{literal}}, true
}

func importedPackageObject(alias, name string, bindings map[string][]ast.Expr) types.Object {
	for _, binding := range bindings["import:"+alias] {
		packagePath, ok := quotedBindingValue(binding)
		if !ok {
			continue
		}
		imported, err := importer.Default().Import(packagePath)
		if err == nil {
			return imported.Scope().Lookup(name)
		}
	}
	return nil
}

func standaloneConstantValue(expression ast.Expr, bindings map[string][]ast.Expr) (constant.Value, bool) {
	source, ok := formatConstantExpression(expression)
	if !ok {
		return nil, false
	}
	result, err := types.Eval(token.NewFileSet(), constantEvaluationPackage(expression, bindings), token.NoPos, source)
	return result.Value, err == nil && result.Value != nil
}

func constantEvaluationPackage(expression ast.Expr, bindings map[string][]ast.Expr) *types.Package {
	neededAliases := map[string]bool{}
	ast.Inspect(expression, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if owner, ok := selector.X.(*ast.Ident); ok {
			neededAliases[owner.Name] = true
		}
		return true
	})
	if len(neededAliases) == 0 && len(bindings["dot-import"]) == 0 {
		return nil
	}
	pkg := types.NewPackage("example.invalid/architecture-constant-evaluation", "architectureconstant")
	loader := importer.Default()
	for alias := range neededAliases {
		for _, binding := range bindings["import:"+alias] {
			packagePath, ok := quotedBindingValue(binding)
			if !ok {
				continue
			}
			imported, err := loader.Import(packagePath)
			if err == nil {
				pkg.Scope().Insert(types.NewPkgName(token.NoPos, pkg, alias, imported))
				break
			}
		}
	}
	for _, binding := range bindings["dot-import"] {
		packagePath, ok := quotedBindingValue(binding)
		if !ok {
			continue
		}
		imported, err := loader.Import(packagePath)
		if err != nil {
			continue
		}
		for _, name := range imported.Scope().Names() {
			if !ast.IsExported(name) {
				continue
			}
			switch object := imported.Scope().Lookup(name).(type) {
			case *types.Const:
				pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, name, object.Type(), object.Val()))
			case *types.TypeName:
				pkg.Scope().Insert(types.NewTypeName(token.NoPos, pkg, name, object.Type()))
			}
		}
	}
	pkg.MarkComplete()
	return pkg
}

func quotedBindingValue(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func formatConstantExpression(expression ast.Expr) (string, bool) {
	var source bytes.Buffer
	if err := format.Node(&source, token.NewFileSet(), expression); err != nil {
		return "", false
	}
	return source.String(), true
}
