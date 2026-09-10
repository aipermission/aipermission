package connectormanagement

import "github.com/aipermission/aipermission/backend/internal/actionresult"

// CredentialBoundary is the connector-management output redaction contract.
// Callers do not need to depend on the underlying action-result implementation.
type CredentialBoundary = actionresult.CredentialBoundary
