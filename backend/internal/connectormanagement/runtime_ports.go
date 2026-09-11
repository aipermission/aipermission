package connectormanagement

import (
	"context"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type CredentialCanonicalizer func(context.Context, string, string, map[string]any) (map[string]any, error)
type CredentialCanonicalizerProvider func(string) CredentialCanonicalizer

func RuntimeCredentialPreparation(runtime workspaceruntime.Port, provider CredentialCanonicalizerProvider) CredentialPreparationPorts {
	return CredentialPreparationPorts{
		Canonicalize: func(ctx context.Context, connectorKind, credentialKind string, public map[string]any) (map[string]any, error) {
			if provider != nil {
				if canonicalize := provider(connectorKind); canonicalize != nil {
					return canonicalize(ctx, connectorKind, credentialKind, public)
				}
			}
			return cloneMap(public), nil
		},
		Decrypt: func(_ context.Context, profileID int64, encrypted string) (map[string]any, error) {
			secret := map[string]any{}
			err := recordcrypto.DecryptJSON(runtime.StoragePort().SecretVault(), runtime.WorkspaceIdentifier(), recordcrypto.ConnectorCredentialProfile, profileID, encrypted, &secret)
			return secret, err
		},
		Encrypt: func(_ context.Context, profileID int64, secret map[string]any) (string, error) {
			return recordcrypto.EncryptJSON(runtime.StoragePort().SecretVault(), runtime.WorkspaceIdentifier(), recordcrypto.ConnectorCredentialProfile, profileID, secret)
		},
	}
}

type RuntimeCapabilities func(string) connectors.RuntimeCapabilityResolver
type ResultRedactor func(context.Context, connectors.ActionResult, CredentialBoundary) (connectors.ActionResult, error)
type TextRedactor func(context.Context, string) string

func RuntimeCredentialPorts(runtime workspaceruntime.Port, capabilities RuntimeCapabilities, redactResult ResultRedactor, redactText TextRedactor) CredentialRuntimePorts {
	return CredentialRuntimePorts{
		DecryptSecret: func(_ context.Context, profileID int64, encrypted string) (map[string]any, error) {
			secret := map[string]any{}
			err := recordcrypto.DecryptJSON(runtime.StoragePort().SecretVault(), runtime.WorkspaceIdentifier(), recordcrypto.ConnectorCredentialProfile, profileID, encrypted, &secret)
			return secret, err
		},
		RuntimeContext: func(target connectortargets.Target, profile connectortargets.CredentialProfile, secrets map[string]any, boundary CredentialBoundary) connectors.RuntimeContext {
			var resolved connectors.RuntimeCapabilityResolver
			if capabilities != nil {
				resolved = capabilities(target.ConnectorKind)
			}
			return connectors.RuntimeContext{
				Target: targetViewForProfile(target, profile.ID), Profile: connectortargets.CredentialProfileView(profile),
				Secrets: secretAccessor{values: secrets, boundary: boundary}, Events: noopEventSink{}, Capabilities: resolved,
			}
		},
		RedactResult: redactResult,
		RedactText:   redactText,
	}
}

type secretAccessor struct {
	values   map[string]any
	boundary CredentialBoundary
}

func (accessor secretAccessor) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := accessor.values[name]
	if !ok || value == nil {
		return "", fmt.Errorf("%w: %q", connectors.ErrSecretNotFound, name)
	}
	text := fmt.Sprint(value)
	accessor.boundary.Add(text)
	return text, nil
}

func (accessor secretAccessor) RegisterSensitiveValue(value string) { accessor.boundary.Add(value) }

type noopEventSink struct{}

func (noopEventSink) Emit(context.Context, connectors.ActionEvent) error { return nil }

func targetViewForProfile(target connectortargets.Target, profileID int64) connectors.TargetView {
	return connectors.TargetView{
		ID: target.ID, Ref: connectors.FormatTargetRef(target.ConnectorKind, target.ID, profileID),
		ConnectorKind: target.ConnectorKind, Name: target.Name, Config: cloneMap(target.Config),
	}
}

func cloneMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
