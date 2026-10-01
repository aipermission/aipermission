package connectorcredentials

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

// RuntimePorts belongs to core credential composition, not to a connector.
// Connectors receive only the resulting context or read-only evidence view.
type RuntimePorts struct {
	DecryptSecret  func(context.Context, int64, string) (map[string]any, error)
	RuntimeContext func(connectortargets.Target, connectortargets.CredentialProfile, map[string]any, actionresult.CredentialBoundary) connectors.RuntimeContext
	RedactResult   func(context.Context, connectors.ActionResult, actionresult.CredentialBoundary) (connectors.ActionResult, error)
	RedactText     func(context.Context, string) string
}

func (ports RuntimePorts) Valid() bool {
	return ports.DecryptSecret != nil && ports.RuntimeContext != nil && ports.RedactResult != nil && ports.RedactText != nil
}

func (ports RuntimePorts) RedactCredentialText(ctx context.Context, value string, boundary actionresult.CredentialBoundary) string {
	return actionresult.RedactCredentialText(value, boundary.Redact, func(text string) string {
		return ports.RedactText(ctx, text)
	})
}
