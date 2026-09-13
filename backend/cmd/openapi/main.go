package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
	"github.com/aipermission/aipermission/backend/internal/restcontract"
)

func main() {
	routesPath := flag.String("routes", "internal/api/httptransport/routes.go", "path to the Go route registration source")
	outputPath := flag.String("output", "../docs/api/openapi.json", "path to the generated OpenAPI document")
	check := flag.Bool("check", false, "verify that the generated document is current")
	flag.Parse()

	source, err := os.ReadFile(*routesPath)
	if err != nil {
		fatalf("read routes: %v", err)
	}
	output, err := generateContract(source)
	if err != nil {
		fatalf("%v", err)
	}
	if *check {
		current, err := os.ReadFile(*outputPath)
		if err != nil {
			fatalf("read generated contract: %v", err)
		}
		if !bytes.Equal(current, output) {
			fatalf("%s is stale; run make rest-contract", *outputPath)
		}
		return
	}
	if err := os.WriteFile(*outputPath, output, 0o644); err != nil {
		fatalf("write generated contract: %v", err)
	}
}

func generateContract(source []byte) ([]byte, error) {
	routes, err := restcontract.ParseRoutes(source)
	if err != nil {
		return nil, fmt.Errorf("parse core routes: %w", err)
	}
	catalog, err := builtin.NewCatalog()
	if err != nil {
		return nil, fmt.Errorf("load built-in connectors: %w", err)
	}
	connectorInfos := catalog.Connectors.List()
	kinds := make([]string, 0, len(connectorInfos))
	for _, info := range connectorInfos {
		kinds = append(kinds, info.Kind)
	}
	adapterRoutes, err := catalog.Adapters.RouteDefinitions(kinds)
	if err != nil {
		return nil, fmt.Errorf("load connector adapter routes: %w", err)
	}
	for _, route := range adapterRoutes {
		routes = append(routes, restcontract.Route{Method: route.Method, Path: route.Path})
	}
	output, err := restcontract.GenerateRoutes(routes)
	if err != nil {
		return nil, fmt.Errorf("generate contract: %w", err)
	}
	return output, nil
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
