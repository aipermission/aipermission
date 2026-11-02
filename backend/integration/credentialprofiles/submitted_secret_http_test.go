package credentialprofiles_test

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"

	management "github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement/profileinput"
	sqlserverconnector "github.com/aipermission/aipermission/backend/internal/connectors/sqlserver"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestCredentialIdentityChangeRequiresFreshSecretOnBothRoutes(t *testing.T) {
	for _, combined := range []bool{false, true} {
		t.Run(fmt.Sprintf("combined=%t", combined), func(t *testing.T) {
			fixture := newManagedMetadataHTTPFixture(t, sqlserverconnector.New())
			handler := fixture.handler
			created := performManagedJSON(t, handler, http.MethodPost, "/api/connector-targets/with-profile", management.CreateTargetWithProfileRequest{
				Target: management.CreateTargetRequest{ConnectorKind: "sqlserver", Name: "My database", Config: map[string]any{
					"connection_mode": "direct", "host": "db.example.test", "port": 1433, "database": "App", "tls_mode": "verify_full",
				}},
				Profile: profileinput.Input{Kind: "username_password", Label: "reader", Public: map[string]any{"username": "reader"}, Secret: map[string]any{"password": "fixture-only"}},
			})
			if created.Code != http.StatusCreated {
				t.Fatalf("create: %d %s", created.Code, created.Body.String())
			}
			item := decodeManagedResponse[management.TargetResponse](t, created.Body.Bytes())
			store := connectortargets.NewStore(fixture.db)
			before, err := store.GetCredentialProfile(t.Context(), item.ID, item.Profiles[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			target, err := store.GetTarget(t.Context(), item.ID)
			if err != nil {
				t.Fatal(err)
			}
			var audits int
			if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM audit_outbox`).Scan(&audits); err != nil {
				t.Fatal(err)
			}
			for _, secret := range []map[string]any{nil, {}, {"password": nil}, {"password": ""}} {
				profile := map[string]any{"kind": before.Kind, "label": "edited", "public": map[string]any{"username": "other"}}
				if secret != nil {
					profile["secret"] = secret
				}
				url := fmt.Sprintf("/api/connector-targets/%d/profiles/%d", item.ID, before.ID)
				var input any = profile
				if combined {
					url = fmt.Sprintf("/api/connector-targets/%d/with-profile/%d", item.ID, before.ID)
					input = map[string]any{"target": management.UpdateTargetRequest{Name: "edited", Config: target.Config}, "profile": profile}
				}
				response := performManagedJSON(t, handler, http.MethodPut, url, input)
				if response.Code != http.StatusBadRequest {
					t.Fatalf("identity change accepted: %d %s", response.Code, response.Body.String())
				}
				after, err := store.GetCredentialProfile(t.Context(), item.ID, before.ID)
				if err != nil {
					t.Fatal(err)
				}
				afterTarget, err := store.GetTarget(t.Context(), item.ID)
				if err != nil {
					t.Fatal(err)
				}
				var afterAudits int
				if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM audit_outbox`).Scan(&afterAudits); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(target, afterTarget) || audits != afterAudits {
					t.Fatal("rejected edit changed persistent state")
				}
			}
		})
	}
}
