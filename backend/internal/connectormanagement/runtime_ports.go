package connectormanagement

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectorcredentials"
	"github.com/aipermission/aipermission/backend/internal/connectorcredentials/profilesecrets"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type CredentialCanonicalizer func(context.Context, string, string, map[string]any) (map[string]any, error)
type CredentialCanonicalizerProvider func(string) CredentialCanonicalizer

type CredentialStorage struct {
	Vault       *vault.Vault
	WorkspaceID string
}

func RuntimeCredentialPreparation(storage CredentialStorage, provider CredentialCanonicalizerProvider) CredentialPreparationPorts {
	codec := profilesecrets.NewProfileSecretCodec(storage.Vault, storage.WorkspaceID)
	return CredentialPreparationPorts{
		Canonicalize: func(ctx context.Context, connectorKind, credentialKind string, public map[string]any) (map[string]any, error) {
			if provider != nil {
				if canonicalize := provider(connectorKind); canonicalize != nil {
					return canonicalize(ctx, connectorKind, credentialKind, public)
				}
			}
			return connectorcredentials.CloneMetadata(public), nil
		},
		Decrypt: codec.Decrypt,
		Encrypt: codec.Encrypt,
	}
}

type RuntimeCapabilities func(string) connectors.RuntimeCapabilityResolver
type ResultRedactor func(context.Context, connectors.ActionResult, CredentialBoundary) (connectors.ActionResult, error)
type TextRedactor func(context.Context, string) string

func RuntimeCredentialPorts(storage CredentialStorage, capabilities RuntimeCapabilities, redactResult ResultRedactor, redactText TextRedactor) CredentialRuntimePorts {
	codec := profilesecrets.NewProfileSecretCodec(storage.Vault, storage.WorkspaceID)
	return CredentialRuntimePorts{
		DecryptSecret: codec.Decrypt,
		RuntimeContext: func(target connectortargets.Target, profile connectortargets.CredentialProfile, secrets map[string]any, boundary CredentialBoundary) connectors.RuntimeContext {
			var resolved connectors.RuntimeCapabilityResolver
			if capabilities != nil {
				resolved = capabilities(target.ConnectorKind)
			}
			return connectorcredentials.Context(target, profile, secrets, boundary, resolved)
		},
		RedactResult: redactResult,
		RedactText:   redactText,
	}
}
