package connectormanagement

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

var ErrCredentialSecretDecode = errors.New("credential secret could not be decoded")

type CredentialInputError struct{ Err error }

func (err CredentialInputError) Error() string {
	if err.Err == nil {
		return "invalid credential profile"
	}
	return err.Err.Error()
}

func (err CredentialInputError) Unwrap() error { return err.Err }

type CredentialProfileInput struct {
	Kind      string         `json:"kind"`
	Label     string         `json:"label"`
	Public    map[string]any `json:"public,omitempty"`
	Secret    map[string]any `json:"secret,omitempty"`
	RiskLabel string         `json:"risk_label,omitempty"`
}

type PreparedCredentialProfile struct {
	Kind          string
	Label         string
	Public        map[string]any
	Secret        map[string]any
	SecretChanged bool
	RiskLabel     string
}

type CredentialPreparationPorts struct {
	Canonicalize func(context.Context, string, string, map[string]any) (map[string]any, error)
	Decrypt      func(context.Context, int64, string) (map[string]any, error)
	Encrypt      func(context.Context, int64, map[string]any) (string, error)
}

func PrepareCredentialProfile(
	ctx context.Context,
	connector connectors.Connector,
	request CredentialProfileInput,
	secretRequired bool,
	previous *connectors.CredentialProfileView,
	previousEncryptedSecret string,
	ports CredentialPreparationPorts,
) (PreparedCredentialProfile, error) {
	kind := strings.TrimSpace(request.Kind)
	schema, ok := CredentialSchemaForKind(connector, kind)
	if !ok {
		return PreparedCredentialProfile{}, CredentialInputError{Err: errors.New("unsupported credential kind")}
	}
	secret, err := mergeCredentialSecrets(ctx, previous, previousEncryptedSecret, request.Secret, ports.Decrypt)
	if err != nil {
		return PreparedCredentialProfile{}, err
	}
	if err := connectors.ValidateCredentialSchemaValues(schema.Schema, request.Public, secret, secretRequired); err != nil {
		return PreparedCredentialProfile{}, CredentialInputError{Err: err}
	}
	public, err := canonicalCredentialPublic(ctx, connector.Kind(), kind, request.Public, ports.Canonicalize)
	if err != nil {
		return PreparedCredentialProfile{}, err
	}
	if err := connectors.ValidateCredentialSchemaValues(schema.Schema, public, secret, secretRequired); err != nil {
		return PreparedCredentialProfile{}, CredentialInputError{Err: err}
	}
	if validator, ok := connector.(connectors.CredentialProfileValidator); ok {
		if err := validator.ValidateCredentialProfile(kind, public, secret, previous); err != nil {
			return PreparedCredentialProfile{}, CredentialInputError{Err: err}
		}
	}
	prepared := PreparedCredentialProfile{
		Kind: kind, Label: request.Label, Public: public, RiskLabel: request.RiskLabel,
	}
	if secretRequired {
		if secret == nil {
			secret = map[string]any{}
		}
		prepared.Secret = secret
		prepared.SecretChanged = true
	} else if request.Secret != nil {
		prepared.Secret = secret
		prepared.SecretChanged = true
	}
	return prepared, nil
}

func EncryptPreparedCredentialSecret(ctx context.Context, profileID int64, prepared PreparedCredentialProfile, ports CredentialPreparationPorts) (*string, error) {
	if !prepared.SecretChanged {
		return nil, nil
	}
	if ports.Encrypt == nil {
		return nil, errors.New("credential secret encryption is unavailable")
	}
	encrypted, err := ports.Encrypt(ctx, profileID, prepared.Secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt connector credential profile: %w", err)
	}
	return &encrypted, nil
}

func CredentialSchemaForKind(connector connectors.Connector, kind string) (connectors.CredentialSchema, bool) {
	if connector == nil || !connectors.ValidIdentifier(kind) {
		return connectors.CredentialSchema{}, false
	}
	for _, schema := range connector.CredentialSchemas() {
		if schema.Kind == kind {
			return schema, true
		}
	}
	return connectors.CredentialSchema{}, false
}

func mergeCredentialSecrets(
	ctx context.Context,
	previous *connectors.CredentialProfileView,
	previousEncrypted string,
	updates map[string]any,
	decrypt func(context.Context, int64, string) (map[string]any, error),
) (map[string]any, error) {
	if updates == nil && previousEncrypted == "" {
		return nil, nil
	}
	merged := map[string]any{}
	if previousEncrypted != "" {
		if previous == nil || previous.ID < 1 || decrypt == nil {
			return nil, fmt.Errorf("%w: previous credential profile identity is unavailable", ErrCredentialSecretDecode)
		}
		decoded, err := decrypt(ctx, previous.ID, previousEncrypted)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCredentialSecretDecode, err)
		}
		for key, value := range decoded {
			merged[key] = value
		}
	}
	for key, value := range updates {
		if value == nil {
			delete(merged, key)
			continue
		}
		merged[key] = value
	}
	return merged, nil
}

func canonicalCredentialPublic(
	ctx context.Context,
	connectorKind, credentialKind string,
	public map[string]any,
	canonicalize func(context.Context, string, string, map[string]any) (map[string]any, error),
) (map[string]any, error) {
	if canonicalize != nil {
		return canonicalize(ctx, connectorKind, credentialKind, public)
	}
	copied := make(map[string]any, len(public))
	for key, value := range public {
		copied[key] = value
	}
	return copied, nil
}
