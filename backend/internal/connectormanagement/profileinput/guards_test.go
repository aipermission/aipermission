package profileinput

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type publicOnlyConnector struct {
	credentialPreparationTestConnector
}

func (publicOnlyConnector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{{Kind: "public", Schema: connectors.Schema{Fields: []connectors.Field{
		{Name: "username", Type: connectors.FieldString, Required: true},
	}}}}
}

func TestPreparationWithoutSecretsReturnsIndependentPublicMap(t *testing.T) {
	input := map[string]any{"username": "operator"}
	prepared, err := Prepare(t.Context(), publicOnlyConnector{}, Input{Kind: "public", Public: input}, true, nil, "", Ports{})
	if err != nil || !prepared.SecretChanged || prepared.Secret == nil || len(prepared.Secret) != 0 {
		t.Fatalf("public-only create=%#v err=%v", prepared, err)
	}
	prepared.Public["username"] = "changed"
	if input["username"] != "operator" {
		t.Fatal("fallback canonicalizer aliased submitted public metadata")
	}
}

func TestStoredSecretRequiresProfileBoundDecoder(t *testing.T) {
	for _, test := range []struct {
		name           string
		previous       *connectors.CredentialProfileView
		missingDecoder bool
	}{
		{name: "missing profile"},
		{name: "zero identity", previous: &connectors.CredentialProfileView{}},
		{name: "negative identity", previous: &connectors.CredentialProfileView{ID: -1}},
		{name: "missing decoder", previous: &connectors.CredentialProfileView{ID: 7}, missingDecoder: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoderCalls := 0
			decoder := func(context.Context, int64, string) (map[string]any, error) {
				decoderCalls++
				return map[string]any{"password": "fixture"}, nil
			}
			if test.missingDecoder {
				decoder = nil
			}
			_, err := Prepare(t.Context(), credentialPreparationTestConnector{}, Input{Kind: "account", Public: map[string]any{"username": "operator"}}, false, test.previous, "stored", Ports{Decrypt: decoder})
			if !errors.Is(err, ErrSecretDecode) || decoderCalls != 0 {
				t.Fatalf("missing identity/decoder error=%v calls=%d", err, decoderCalls)
			}
		})
	}
}

func TestEncryptionSkipsUnchangedSecretsAndPreservesFailure(t *testing.T) {
	called := false
	result, err := EncryptSecret(t.Context(), 7, Prepared{}, Ports{Encrypt: func(context.Context, int64, map[string]any) (string, error) {
		called = true
		return "must-not-use", nil
	}})
	if err != nil || result != nil || called {
		t.Fatalf("unchanged secret: result=%v called=%v err=%v", result, called, err)
	}
	result, err = EncryptSecret(t.Context(), 7, Prepared{SecretChanged: true}, Ports{})
	if err == nil || result != nil {
		t.Fatal("changed secret encrypted without a port")
	}
	cause := errors.New("encryption failed")
	result, err = EncryptSecret(t.Context(), 7, Prepared{SecretChanged: true}, Ports{Encrypt: func(context.Context, int64, map[string]any) (string, error) {
		return "partial ciphertext", cause
	}})
	if result != nil || !errors.Is(err, cause) {
		t.Fatalf("encryption failure: result=%v err=%v", result, err)
	}
}

func TestSchemaLookupRejectsMissingConnectorAndMalformedKind(t *testing.T) {
	if _, found := SchemaForKind(nil, "account"); found {
		t.Fatal("schema found without connector")
	}
	if _, found := SchemaForKind(credentialPreparationTestConnector{}, "invalid kind"); found {
		t.Fatal("schema found for malformed kind")
	}
}
