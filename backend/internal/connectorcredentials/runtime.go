// Package connectorcredentials constructs core-bound credential snapshots.
package connectorcredentials

import (
	"context"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

// Context preserves the current target/profile authority without exposing
// record decryption, database, or Vault ports to the connector implementation.
func Context(target connectortargets.Target, profile connectortargets.CredentialProfile, secrets map[string]any, boundary actionresult.CredentialBoundary, capabilities connectors.RuntimeCapabilityResolver) connectors.RuntimeContext {
	return connectors.RuntimeContext{
		Target:  TargetView(target, profile.ID),
		Profile: connectortargets.CredentialProfileView(profile),
		Secrets: Secrets(secrets, boundary), Events: EventSink{}, Capabilities: capabilities,
	}
}

// TargetView supplies the same public authority snapshot to execution and
// resource-only evidence readers without granting either secret access.
func TargetView(target connectortargets.Target, profileID int64) connectors.TargetView {
	return connectors.TargetView{
		ID: target.ID, Ref: connectors.FormatTargetRef(target.ConnectorKind, target.ID, profileID),
		ProjectID: target.ProjectID, ConnectorKind: target.ConnectorKind, Name: target.Name,
		Config: CloneMetadata(target.Config), UpdatedAt: target.UpdatedAt,
	}
}

// CloneMetadata preserves an empty object for nil input and isolates the map's
// top-level keys; nested values retain the existing immutable snapshot contract.
func CloneMetadata(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

type secretAccessor struct {
	values   map[string]any
	boundary actionresult.CredentialBoundary
}

func Secrets(values map[string]any, boundary actionresult.CredentialBoundary) connectors.SecretAccessor {
	return secretAccessor{values: values, boundary: boundary}
}

func (accessor secretAccessor) GetSecret(ctx context.Context, name string) (string, error) {
	if ctx == nil {
		return "", errors.New("credential secret context is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, ok := accessor.values[name]
	if !ok || value == nil {
		return "", fmt.Errorf("%w: %q", connectors.ErrSecretNotFound, name)
	}
	text := fmt.Sprint(value)
	accessor.boundary.Add(text)
	return text, nil
}

func (accessor secretAccessor) RegisterSensitiveValue(value string) { accessor.boundary.Add(value) }

type EventSink struct{}

func (EventSink) Emit(context.Context, connectors.ActionEvent) error { return nil }
