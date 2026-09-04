package restcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type Route struct {
	Method string
	Path   string
}

func ParseRoutes(source []byte) ([]Route, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "routes.go", source, 0)
	if err != nil {
		return nil, fmt.Errorf("parse routes source: %w", err)
	}
	routes := []Route{}
	ast.Inspect(file, func(node ast.Node) bool {
		if err != nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "Handle" {
			err = fmt.Errorf("unsupported Handle route registration; use HandleFunc so the route contract can be generated")
			return false
		}
		if selector.Sel.Name != "HandleFunc" {
			return true
		}
		if len(call.Args) == 0 {
			err = fmt.Errorf("HandleFunc route registration has no pattern")
			return false
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			err = fmt.Errorf("HandleFunc route pattern must be a string literal")
			return false
		}
		pattern, unquoteErr := strconv.Unquote(literal.Value)
		if unquoteErr != nil {
			err = fmt.Errorf("decode route pattern %s: %w", literal.Value, unquoteErr)
			return false
		}
		method, path, ok := strings.Cut(strings.TrimSpace(pattern), " ")
		if !ok || method == "" || !strings.HasPrefix(path, "/") {
			err = fmt.Errorf("invalid route pattern %q", pattern)
			return false
		}
		routes = append(routes, Route{Method: strings.ToUpper(method), Path: path})
		return true
	})
	if err != nil {
		return nil, err
	}
	return NormalizeRoutes(routes)
}

// NormalizeRoutes validates, sorts, and deduplicates a combined core and
// connector-owned route inventory.
func NormalizeRoutes(routes []Route) ([]Route, error) {
	if len(routes) == 0 {
		return nil, fmt.Errorf("no routes found")
	}
	normalized := make([]Route, 0, len(routes))
	for _, route := range routes {
		route.Method = strings.ToUpper(strings.TrimSpace(route.Method))
		route.Path = strings.TrimSpace(route.Path)
		if route.Method == "" || !strings.HasPrefix(route.Path, "/") {
			return nil, fmt.Errorf("invalid route %q %q", route.Method, route.Path)
		}
		normalized = append(normalized, route)
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Path == normalized[j].Path {
			return normalized[i].Method < normalized[j].Method
		}
		return normalized[i].Path < normalized[j].Path
	})
	for index := 1; index < len(normalized); index++ {
		if normalized[index] == normalized[index-1] {
			return nil, fmt.Errorf("duplicate route %s %s", normalized[index].Method, normalized[index].Path)
		}
	}
	return normalized, nil
}

func Generate(source []byte) ([]byte, error) {
	routes, err := ParseRoutes(source)
	if err != nil {
		return nil, err
	}
	return GenerateRoutes(routes)
}

// GenerateRoutes emits OpenAPI from a combined core and connector-owned route
// inventory.
func GenerateRoutes(routes []Route) ([]byte, error) {
	routes, err := NormalizeRoutes(routes)
	if err != nil {
		return nil, err
	}
	paths := map[string]map[string]any{}
	operationIDs := map[string]Route{}
	typedContracts := typedOperationContracts()
	for _, route := range routes {
		operationID := routeOperationID(route)
		if previous, exists := operationIDs[operationID]; exists {
			return nil, fmt.Errorf("operation id %q collides for %s %s and %s %s", operationID, previous.Method, previous.Path, route.Method, route.Path)
		}
		operationIDs[operationID] = route
		responses := map[string]any{
			"default": responseWithSchema("Error response", refSchema("Error")),
		}
		contractLevel := "route-inventory"
		if contract, ok := typedContracts[route]; ok {
			responses[contract.StatusCode] = responseWithSchema("Successful response", contract.ResponseSchema)
			contractLevel = "typed-response"
			for status, schema := range contract.AdditionalResponses {
				responses[status] = responseWithSchema("Documented error response", schema)
			}
		}
		operation := map[string]any{
			"operationId":                   operationID,
			"responses":                     responses,
			"tags":                          []string{routeTag(route.Path)},
			"x-aipermission-contract-level": contractLevel,
		}
		if parameters := pathParameters(route.Path); len(parameters) > 0 {
			operation["parameters"] = parameters
		}
		if contract, ok := typedContracts[route]; ok && contract.RequestSchema != nil {
			operation["requestBody"] = map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{"schema": contract.RequestSchema},
				},
			}
			operation["x-aipermission-contract-level"] = "typed-request-response"
		}
		if paths[route.Path] == nil {
			paths[route.Path] = map[string]any{}
		}
		paths[route.Path][strings.ToLower(route.Method)] = operation
	}
	spec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "AIPermission Local REST API",
			"version":     "typed-contract-v1",
			"description": "Generated local REST contract. Shared response families are typed incrementally; remaining operations are explicitly marked as route inventory.",
		},
		"servers":                       []map[string]string{{"url": "http://localhost:3210"}},
		"components":                    map[string]any{"schemas": sharedSchemas()},
		"paths":                         paths,
		"x-aipermission-generated-from": "core and connector adapter route catalogs",
		"x-aipermission-contract-level": "route-inventory",
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(spec); err != nil {
		return nil, fmt.Errorf("encode OpenAPI route inventory: %w", err)
	}
	return output.Bytes(), nil
}

func responseWithSchema(description string, schema map[string]any) map[string]any {
	return map[string]any{
		"description": description,
		"content": map[string]any{
			"application/json": map[string]any{"schema": schema},
		},
	}
}

func routeOperationID(route Route) string {
	parts := splitIdentifier(route.Path)
	var output strings.Builder
	output.WriteString(strings.ToLower(route.Method))
	for _, part := range parts {
		if strings.HasPrefix(part, "{") {
			output.WriteString("By")
			part = strings.Trim(part, "{}")
		}
		output.WriteString(upperCamel(part))
	}
	return output.String()
}

func splitIdentifier(value string) []string {
	return strings.FieldsFunc(value, func(character rune) bool {
		return !(unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '{' || character == '}')
	})
}

func upperCamel(value string) string {
	parts := strings.FieldsFunc(value, func(character rune) bool {
		return !(unicode.IsLetter(character) || unicode.IsDigit(character))
	})
	for index, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(strings.ToLower(part))
		runes[0] = unicode.ToUpper(runes[0])
		parts[index] = string(runes)
	}
	return strings.Join(parts, "")
}

func routeTag(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "root"
	}
	if parts[0] == "api" && len(parts) > 1 {
		return parts[1]
	}
	return parts[0]
}

func pathParameters(path string) []map[string]any {
	parameters := []map[string]any{}
	for _, part := range strings.Split(path, "/") {
		if !strings.HasPrefix(part, "{") || !strings.HasSuffix(part, "}") {
			continue
		}
		name := strings.Trim(part, "{}")
		typeName := "string"
		if name == "id" || strings.HasSuffix(name, "_id") {
			typeName = "integer"
		}
		parameters = append(parameters, map[string]any{
			"name": name, "in": "path", "required": true,
			"schema": map[string]string{"type": typeName},
		})
	}
	return parameters
}
