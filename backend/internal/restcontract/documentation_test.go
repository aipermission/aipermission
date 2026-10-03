package restcontract

import (
	"strings"
	"testing"
)

func TestDocumentedRoutesRequireExactMethodAndPath(t *testing.T) {
	routes := []Route{{Method: "GET", Path: "/api/approvals/{id}"}, {Method: "POST", Path: "/api/approvals/{id}/run"}}
	for _, documentation := range []string{
		"POST /api/approvals/{id}/run",
		"POST /api/approvals/{id}\nPOST /api/approvals/{id}/run",
		"GET /api/approvals/{id}/run\nPOST /api/approvals/{id}/run",
		"GET /api/approvals/{id}?detail=true\nPOST /api/approvals/{id}/run",
		"Text mentions GET /api/approvals/{id}\nPOST /api/approvals/{id}/run",
	} {
		if err := ValidateDocumentedRoutes(routes, documentation); err == nil || !strings.Contains(err.Error(), "GET /api/approvals/{id}") {
			t.Fatalf("inexact route mention accepted: %q error %v", documentation, err)
		}
	}
	if err := ValidateDocumentedRoutes(routes, "```txt\n GET  /api/approvals/{id} \nPOST\t/api/approvals/{id}/run\n```"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocumentedRoutes(nil, ""); err != nil {
		t.Fatal(err)
	}
}
