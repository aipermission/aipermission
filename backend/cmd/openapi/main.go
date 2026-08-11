package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/aipermission/aipermission/backend/internal/restcontract"
)

func main() {
	routesPath := flag.String("routes", "internal/api/routes.go", "path to the Go route registration source")
	outputPath := flag.String("output", "../docs/api/openapi.json", "path to the generated OpenAPI document")
	check := flag.Bool("check", false, "verify that the generated document is current")
	flag.Parse()

	source, err := os.ReadFile(*routesPath)
	if err != nil {
		fatalf("read routes: %v", err)
	}
	output, err := restcontract.Generate(source)
	if err != nil {
		fatalf("generate contract: %v", err)
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

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
