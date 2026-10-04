package profileinput

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type validatingPreparationConnector struct {
	credentialPreparationTestConnector
	validate func(string, map[string]any, map[string]any, *connectors.CredentialProfileView) error
}

func (connector validatingPreparationConnector) ValidateCredentialProfile(kind string, public, secret map[string]any, previous *connectors.CredentialProfileView) error {
	return connector.validate(kind, public, secret, previous)
}

func TestCredentialPreparationPreservesValidationOrderAndIdentity(t *testing.T) {
	ctx := context.WithValue(t.Context(), struct{}{}, "caller")
	previous := &connectors.CredentialProfileView{ID: 9007199254740993}
	decoded := map[string]any{"password": "original", "session_token": "remove"}
	updates := map[string]any{"session_token": nil}
	public := map[string]any{"username": "raw"}
	var calls []string
	connector := validatingPreparationConnector{validate: func(kind string, public, secret map[string]any, old *connectors.CredentialProfileView) error {
		calls = append(calls, "validate")
		if kind != "account" || public["username"] != "canonical" || secret["password"] != "original" || old != previous {
			t.Fatalf("validator identity/input: %s %#v %#v %p", kind, public, secret, old)
		}
		return nil
	}}
	ports := Ports{
		Decrypt: func(received context.Context, id int64, encrypted string) (map[string]any, error) {
			calls = append(calls, "decrypt")
			if received != ctx || id != previous.ID || encrypted != "stored" {
				t.Fatal("decrypt changed caller context or exact identity")
			}
			return decoded, nil
		},
		Canonicalize: func(received context.Context, connectorKind, credentialKind string, input map[string]any) (map[string]any, error) {
			calls = append(calls, "canonicalize")
			if received != ctx || connectorKind != connector.Kind() || credentialKind != "account" || input["username"] != "raw" {
				t.Fatal("canonicalizer received different caller input")
			}
			return map[string]any{"username": "canonical"}, nil
		},
		Encrypt: func(received context.Context, id int64, input map[string]any) (string, error) {
			calls = append(calls, "encrypt")
			if received != ctx || id != previous.ID || input["password"] != "original" || len(input) != 1 {
				t.Fatal("encrypt changed caller context, exact identity or merged secret")
			}
			return "replacement", nil
		},
	}
	prepared, err := Prepare(ctx, connector, Input{
		Kind: " account ", Label: "Exact label ", Public: public, Secret: updates, RiskLabel: "read",
	}, false, previous, "stored", ports)
	if err != nil || !reflect.DeepEqual(calls, []string{"decrypt", "canonicalize", "validate"}) {
		t.Fatalf("prepare order=%v err=%v", calls, err)
	}
	if prepared.Label != "Exact label " || prepared.RiskLabel != "read" || !prepared.SecretChanged {
		t.Fatalf("prepared metadata=%#v", prepared)
	}
	if !reflect.DeepEqual(decoded, map[string]any{"password": "original", "session_token": "remove"}) || len(updates) != 1 || public["username"] != "raw" {
		t.Fatal("preparation mutated caller-owned maps")
	}
	encrypted, err := EncryptSecret(ctx, previous.ID, prepared, ports)
	if err != nil || encrypted == nil || *encrypted != "replacement" || calls[len(calls)-1] != "encrypt" {
		t.Fatalf("encryption=%v calls=%v err=%v", encrypted, calls, err)
	}
}

func TestCredentialPreparationRejectsBeforeSemanticValidation(t *testing.T) {
	for _, test := range []struct {
		name           string
		public         map[string]any
		canonical      map[string]any
		canonicalError error
		wantCanonical  int
		wantCause      error
	}{
		{name: "submitted invalid", public: map[string]any{"username": 12}, wantCanonical: 0},
		{name: "canonical invalid", public: map[string]any{"username": "raw"}, canonical: map[string]any{"username": 12}, wantCanonical: 1},
		{name: "canonical canceled", public: map[string]any{"username": "raw"}, canonicalError: context.Canceled, wantCanonical: 1, wantCause: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			canonicalCalls, validatorCalls := 0, 0
			connector := validatingPreparationConnector{validate: func(string, map[string]any, map[string]any, *connectors.CredentialProfileView) error {
				validatorCalls++
				return nil
			}}
			_, err := Prepare(t.Context(), connector, Input{
				Kind: "account", Public: test.public, Secret: map[string]any{"password": "fixture"},
			}, true, nil, "", Ports{Canonicalize: func(context.Context, string, string, map[string]any) (map[string]any, error) {
				canonicalCalls++
				return test.canonical, test.canonicalError
			}})
			if err == nil || canonicalCalls != test.wantCanonical || validatorCalls != 0 {
				t.Fatalf("err=%v canonical=%d validator=%d", err, canonicalCalls, validatorCalls)
			}
			if test.wantCause != nil {
				if !errors.Is(err, test.wantCause) {
					t.Fatalf("lost cause: %v", err)
				}
			} else {
				var inputErr InputError
				if !errors.As(err, &inputErr) {
					t.Fatalf("wrong error classification: %T", err)
				}
			}
		})
	}
}

func TestCredentialPreparationRetainsSemanticErrorClassification(t *testing.T) {
	cause := errors.New("semantic rejection")
	connector := validatingPreparationConnector{validate: func(string, map[string]any, map[string]any, *connectors.CredentialProfileView) error { return cause }}
	_, err := Prepare(t.Context(), connector, Input{
		Kind: "account", Public: map[string]any{"username": "raw"}, Secret: map[string]any{"password": "fixture"},
	}, true, nil, "", Ports{})
	var inputErr InputError
	if !errors.Is(err, cause) || !errors.As(err, &inputErr) {
		t.Fatalf("semantic error classification=%T %v", err, err)
	}
}
