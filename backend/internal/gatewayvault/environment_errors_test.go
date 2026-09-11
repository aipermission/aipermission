package gatewayvault

import (
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

func TestClassifySessionEnvironmentError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		kind    SessionEnvironmentErrorKind
		message string
		ok      bool
	}{
		{name: "validation", err: projectvault.ValidationError("invalid item"), kind: SessionEnvironmentValidation, message: "invalid item", ok: true},
		{name: "not found", err: projectvault.ErrNotFound, kind: SessionEnvironmentNotFound, message: "vault item not found", ok: true},
		{name: "stale", err: projectvault.ErrStale, kind: SessionEnvironmentStale, message: projectvault.ErrStale.Error(), ok: true},
		{name: "unknown", err: errors.New("unknown")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kind, message, ok := ClassifySessionEnvironmentError(test.err)
			if kind != test.kind || message != test.message || ok != test.ok {
				t.Fatalf("classification = (%d, %q, %t)", kind, message, ok)
			}
		})
	}
}
