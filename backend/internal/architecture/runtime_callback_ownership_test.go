package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestConnectorRuntimeDoesNotStoreUpperLayerWorkflowCallbacks(t *testing.T) {
	path := filepath.Join("..", "gatewayinfrastructure", "connector_runtime_application.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{"Finish": true, "Delete": true, "Finalize": true}
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if !ok || (typeSpec.Name.Name != "ConnectorRuntimeDependencies" && typeSpec.Name.Name != "ConnectorRuntimeApplication") {
				continue
			}
			structure, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range structure.Fields.List {
				for _, name := range field.Names {
					if forbidden[name.Name] {
						t.Errorf("%s stores upper-layer %s callback; pass workflow ownership at invocation time", typeSpec.Name.Name, name.Name)
					}
				}
			}
		}
	}
}
