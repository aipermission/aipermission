package profileinput

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type submittedPreparationConnector struct {
	validatingPreparationConnector
	submitted func(string, map[string]any, map[string]any, *connectors.CredentialProfileView) error
}

func (c submittedPreparationConnector) ValidateCredentialProfileInput(kind string, public, secret map[string]any, previous *connectors.CredentialProfileView) error {
	return c.submitted(kind, public, secret, previous)
}

func TestSubmittedAndEffectiveSecretValidationAreDistinct(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "rejected"}[rejected], func(t *testing.T) {
			var calls []string
			cause := errors.New("submitted intent rejected")
			previous := &connectors.CredentialProfileView{ID: 7}
			updates := map[string]any{"session_token": nil}
			c := submittedPreparationConnector{
				validatingPreparationConnector: validatingPreparationConnector{validate: func(_ string, _ map[string]any, secret map[string]any, _ *connectors.CredentialProfileView) error {
					calls = append(calls, "effective")
					if !reflect.DeepEqual(secret, map[string]any{"password": "saved"}) {
						t.Fatal("effective secret not merged")
					}
					return nil
				}},
				submitted: func(kind string, public, secret map[string]any, old *connectors.CredentialProfileView) error {
					calls = append(calls, "submitted")
					if kind != "account" || public["username"] != "canonical" || !reflect.DeepEqual(secret, updates) || old != previous {
						t.Fatal("submitted identity or intent changed")
					}
					if rejected {
						return cause
					}
					return nil
				},
			}
			_, err := Prepare(t.Context(), c, Input{Kind: "account", Public: map[string]any{"username": "raw"}, Secret: updates}, false, previous, "encrypted", Ports{
				Decrypt: func(context.Context, int64, string) (map[string]any, error) {
					return map[string]any{"password": "saved", "session_token": "old"}, nil
				},
				Canonicalize: func(context.Context, string, string, map[string]any) (map[string]any, error) {
					return map[string]any{"username": "canonical"}, nil
				},
			})
			want := []string{"submitted", "effective"}
			if rejected {
				want = want[:1]
				var inputErr InputError
				if !errors.Is(err, cause) || !errors.As(err, &inputErr) {
					t.Fatalf("lost submitted error: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, want) || !reflect.DeepEqual(updates, map[string]any{"session_token": nil}) {
				t.Fatalf("calls=%v updates=%v", calls, updates)
			}
		})
	}
}
