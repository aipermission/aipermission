package profileinput

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type credentialPreparationTestConnector struct{ connectors.Connector }

func (credentialPreparationTestConnector) Kind() string { return "preparation_fixture" }

func (credentialPreparationTestConnector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{{
		Kind: "account", Label: "Account",
		Schema: connectors.Schema{Fields: []connectors.Field{
			{Name: "username", Type: connectors.FieldString, Required: true},
			{Name: "password", Type: connectors.FieldSecret, Required: true, Secret: true},
			{Name: "session_token", Type: connectors.FieldSecret, Secret: true},
		}},
	}}
}

func TestPrepareCredentialProfilePreservesAndRemovesPreviousSecrets(t *testing.T) {
	previous := &connectors.CredentialProfileView{ID: 7}
	ports := Ports{
		Decrypt: func(_ context.Context, profileID int64, encrypted string) (map[string]any, error) {
			if profileID != 7 || encrypted != "encrypted" {
				t.Fatalf("decrypt identity = %d %q", profileID, encrypted)
			}
			return map[string]any{"password": "old-password", "session_token": "temporary"}, nil
		},
	}
	prepared, err := Prepare(t.Context(), credentialPreparationTestConnector{}, Input{
		Kind: "account", Label: "main", Public: map[string]any{"username": "operator"},
		Secret: map[string]any{"password": "new-password", "session_token": nil},
	}, true, previous, "encrypted", ports)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Secret["password"] != "new-password" || !prepared.SecretChanged {
		t.Fatalf("prepared secret = %#v", prepared)
	}
	if _, exists := prepared.Secret["session_token"]; exists {
		t.Fatalf("removed secret survived: %#v", prepared.Secret)
	}
}

func TestPrepareCredentialProfileLoadsSecretsForMetadataOnlyValidation(t *testing.T) {
	prepared, err := Prepare(t.Context(), credentialPreparationTestConnector{}, Input{
		Kind: "account", Label: "renamed", Public: map[string]any{"username": "operator"},
	}, false, &connectors.CredentialProfileView{ID: 7}, "encrypted", Ports{
		Decrypt: func(context.Context, int64, string) (map[string]any, error) {
			return map[string]any{"password": "preserved"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.SecretChanged || prepared.Secret != nil {
		t.Fatalf("metadata-only edit should not rewrite secret: %#v", prepared)
	}
}

func TestPrepareCredentialProfileClassifiesInputAndSecretDecodeErrors(t *testing.T) {
	_, err := Prepare(t.Context(), credentialPreparationTestConnector{}, Input{Kind: "missing"}, true, nil, "", Ports{})
	var inputErr InputError
	if !errors.As(err, &inputErr) || inputErr.Error() != "unsupported credential kind" {
		t.Fatalf("unsupported kind error = %T %v", err, err)
	}
	_, err = Prepare(t.Context(), credentialPreparationTestConnector{}, Input{
		Kind: "account", Public: map[string]any{"username": "operator"},
	}, false, &connectors.CredentialProfileView{ID: 7}, "encrypted", Ports{
		Decrypt: func(context.Context, int64, string) (map[string]any, error) { return nil, errors.New("decode failed") },
	})
	if !errors.Is(err, ErrSecretDecode) || strings.Contains(err.Error(), "operator") {
		t.Fatalf("secret decode error = %v", err)
	}
}

func TestCredentialInputErrorDefaultsAndUnwraps(t *testing.T) {
	if got := (InputError{}).Error(); got != "invalid credential profile" {
		t.Fatalf("default error=%q", got)
	}
	cause := errors.New("invalid account")
	wrapped := InputError{Err: cause}
	if !errors.Is(wrapped, cause) {
		t.Fatalf("wrapped error=%v", wrapped)
	}
}

func TestEncryptPreparedCredentialSecretUsesProfileBoundPort(t *testing.T) {
	called := false
	encrypted, err := EncryptSecret(t.Context(), 9, Prepared{
		Secret: map[string]any{"password": "secret"}, SecretChanged: true,
	}, Ports{
		Encrypt: func(_ context.Context, profileID int64, secret map[string]any) (string, error) {
			called = true
			if profileID != 9 || secret["password"] != "secret" {
				t.Fatalf("encrypt input = %d %#v", profileID, secret)
			}
			return "ciphertext", nil
		},
	})
	if err != nil || !called || encrypted == nil || *encrypted != "ciphertext" {
		t.Fatalf("encrypted=%v called=%v err=%v", encrypted, called, err)
	}
}
