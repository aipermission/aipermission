package sqlserverconnector

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement/profileinput"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestCredentialPreparationRequiresSubmittedPasswordForIdentityChange(t *testing.T) {
	previous := &connectors.CredentialProfileView{ID: 7, Kind: "username_password", Public: map[string]any{"username": "reader"}}
	for _, test := range []struct {
		name, username string
		secret         map[string]any
		accepted       bool
	}{
		{"unchanged omitted", "reader", nil, true},
		{"unchanged empty object", "reader", map[string]any{}, true},
		{"changed omitted", "other", nil, false},
		{"changed empty object", "other", map[string]any{}, false},
		{"changed deleted", "other", map[string]any{"password": nil}, false},
		{"changed blank", "other", map[string]any{"password": ""}, false},
		{"changed replacement", "other", map[string]any{"password": "replacement"}, true},
		{"changed explicitly reused", "other", map[string]any{"password": "saved-fixture"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := profileinput.Prepare(t.Context(), New(), profileinput.Input{Kind: previous.Kind,
				Public: map[string]any{"username": test.username}, Secret: test.secret}, false, previous, "encrypted", profileinput.Ports{
				Decrypt: func(context.Context, int64, string) (map[string]any, error) {
					return map[string]any{"password": "saved-fixture"}, nil
				},
			})
			if (err == nil) != test.accepted {
				t.Fatalf("accepted=%t error=%v", test.accepted, err)
			}
			var inputErr profileinput.InputError
			if err != nil && !errors.As(err, &inputErr) {
				t.Fatalf("lost input classification: %v", err)
			}
		})
	}
}
