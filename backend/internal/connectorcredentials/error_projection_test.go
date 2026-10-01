package connectorcredentials

import (
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
)

func TestAuditErrorProjectionKeepsMandatoryMaskingBeforeBasicPolicy(t *testing.T) {
	secret := "alpha violet-suffix-7291"
	boundary := actionresult.NewCredentialBoundary(map[string]any{"password": secret})
	if got := RedactErrorForAudit(nil, boundary); got != "" {
		t.Fatalf("nil error projected as %q", got)
	}
	if got := RedactErrorForAudit(errors.New("password="+secret), boundary); got != "password="+actionresult.CredentialRedactionMarker {
		t.Fatalf("known credential masking order changed: %q", got)
	}
	if got := RedactErrorForAudit(errors.New("password=unregistered-secret"), actionresult.NewCredentialBoundary(nil)); strings.Contains(got, "unregistered-secret") {
		t.Fatalf("basic policy was omitted: %q", got)
	}
}
