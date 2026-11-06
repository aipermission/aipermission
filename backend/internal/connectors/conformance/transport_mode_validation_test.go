package conformance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	rabbit "github.com/aipermission/aipermission/backend/internal/connectors/rabbitmq"
	redis "github.com/aipermission/aipermission/backend/internal/connectors/redis"
)

func TestMalformedTransportModesRejectBeforeCredentialsOrDial(t *testing.T) {
	for _, fixture := range []struct {
		connector     connectors.Connector
		field, action string
		want          error
	}{
		{redis.New(), "tls_mode", redis.ActionPing, redis.ErrInvalidConfig},
		{rabbit.New(), "scheme", rabbit.ActionOverview, rabbit.ErrInvalidConfig},
	} {
		for _, mode := range []string{"direct", "over_ssh"} {
			for _, value := range []any{"bogus", "HTTPS", true, 1, map[string]any{}} {
				t.Run(fixture.connector.Kind()+"/"+mode+"/"+fixture.field, func(t *testing.T) {
					target := connectors.TargetView{Config: map[string]any{fixture.field: value, "host": "remote.invalid", "connection_mode": mode}}
					prepared, err := fixture.connector.PrepareAction(t.Context(), connectors.ActionRequest{Target: target, ActionName: fixture.action, Input: map[string]any{}})
					if err != nil {
						t.Fatal(err)
					}
					secret := &rejectingTransportModeSecret{t: t}
					_, err = fixture.connector.ExecuteAction(t.Context(), connectors.RuntimeContext{Target: target, Secrets: secret}, prepared)
					if !errors.Is(err, fixture.want) {
						t.Fatalf("invalid transport config reached transport/credentials: %v", err)
					}
				})
			}
		}
	}
}

type rejectingTransportModeSecret struct{ t *testing.T }

func (secret *rejectingTransportModeSecret) GetSecret(context.Context, string) (string, error) {
	secret.t.Fatal("invalid transport configuration accessed a secret")
	return "", nil
}
