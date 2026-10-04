package postgres_test

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"

	management "github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement/profileinput"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func managedMetadataTargetRequest(name string) management.CreateTargetRequest {
	return management.CreateTargetRequest{
		ConnectorKind: "postgres", Name: name,
		Config: map[string]any{
			"connection_mode": "direct", "host": "127.0.0.1", "port": 5432,
			"database": "app", "ssl_mode": "require",
		},
	}
}

func TestManagedMetadataHTTPCreateCannotAssertConnectorOwnership(t *testing.T) {
	fixture := newManagedMetadataHTTPFixture(t)
	handler := fixture.handler
	response := performManagedJSON(t, handler, http.MethodPost, "/api/connector-targets", managedMetadataTargetRequest("metadata-target"))
	if response.Code != http.StatusCreated {
		t.Fatalf("create target: %d %s", response.Code, response.Body.String())
	}
	target := decodeManagedResponse[management.TargetResponse](t, response.Body.Bytes())
	path := fmt.Sprintf("/api/connector-targets/%d/profiles", target.ID)
	for field, value := range map[string]any{
		"managed_by_aipermission": true, "managed_role_name": "reader",
		"managed_admin_profile_id": 3, "managed_admin_profile_ref": "admin",
		"managed_preset": "read_only", "managed_scope": map[string]any{"preset": "read_only"},
	} {
		t.Run(field, func(t *testing.T) {
			request := profileinput.Input{
				Kind: "username_password", Label: "forged",
				Public: map[string]any{"username": "reader", field: value},
				Secret: map[string]any{"password": "metadata-test-only"},
			}
			for _, combined := range []bool{false, true} {
				var resultCode int
				if combined {
					result := performManagedJSON(t, handler, http.MethodPost, "/api/connector-targets/with-profile", management.CreateTargetWithProfileRequest{
						Target: managedMetadataTargetRequest("forged-target"), Profile: request,
					})
					resultCode = result.Code
				} else {
					resultCode = performManagedJSON(t, handler, http.MethodPost, path, request).Code
				}
				if resultCode != http.StatusBadRequest {
					t.Fatalf("combined=%v accepted reserved metadata: %d", combined, resultCode)
				}
			}
		})
	}
	var profiles, targets int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM connector_credential_profiles`).Scan(&profiles); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM connector_targets`).Scan(&targets); err != nil {
		t.Fatal(err)
	}
	if profiles != 0 || targets != 1 {
		t.Fatalf("rejected requests persisted state: profiles=%d targets=%d", profiles, targets)
	}
	control := performManagedJSON(t, handler, http.MethodPost, path, profileinput.Input{
		Kind: "username_password", Label: "ordinary",
		Public: map[string]any{"username": "reader"}, Secret: map[string]any{"password": "metadata-test-only"},
	})
	if control.Code != http.StatusCreated {
		t.Fatalf("ordinary credential create failed: %d %s", control.Code, control.Body.String())
	}
}

func TestManagedMetadataHTTPUpdatesPreserveExactStoredOwnership(t *testing.T) {
	fixture := newManagedMetadataHTTPFixture(t)
	handler := fixture.handler
	response := performManagedJSON(t, handler, http.MethodPost, "/api/connector-targets/with-profile", management.CreateTargetWithProfileRequest{
		Target: managedMetadataTargetRequest("metadata-update"),
		Profile: profileinput.Input{
			Kind: "username_password", Label: "reader",
			Public: map[string]any{"username": "reader"}, Secret: map[string]any{"password": "metadata-test-only"},
		},
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create fixture: %d %s", response.Code, response.Body.String())
	}
	target := decodeManagedResponse[management.TargetResponse](t, response.Body.Bytes())
	store := connectortargets.NewStore(fixture.db)
	profileID := target.Profiles[0].ID
	path := fmt.Sprintf("/api/connector-targets/%d/profiles/%d", target.ID, profileID)
	combinedPath := fmt.Sprintf("/api/connector-targets/%d/with-profile/%d", target.ID, profileID)
	update := func(public map[string]any, combined bool) int {
		t.Helper()
		request := profileinput.Input{Kind: "username_password", Label: "reader", Public: public}
		if !combined {
			return performManagedJSON(t, handler, http.MethodPut, path, request).Code
		}
		return performManagedJSON(t, handler, http.MethodPut, combinedPath, management.UpdateTargetWithProfileRequest{
			Target: management.UpdateTargetRequest{Name: target.Name, Config: target.Config}, Profile: request,
		}).Code
	}
	for _, combined := range []bool{false, true} {
		if code := update(map[string]any{
			"username": "reader", "managed_by_aipermission": true,
			"managed_role_name": "reader", "managed_admin_profile_id": 3,
		}, combined); code != http.StatusOK {
			t.Fatalf("ordinary edit failed: combined=%v code=%d", combined, code)
		}
		profile, err := store.GetCredentialProfile(t.Context(), target.ID, profileID)
		if err != nil || !reflect.DeepEqual(profile.Public, map[string]any{"username": "reader"}) {
			t.Fatalf("ordinary edit promoted ownership: %#v err=%v", profile.Public, err)
		}
	}
	// Provisioning owns this write; the normal HTTP create route cannot seed it.
	seeded, err := store.UpdateCredentialProfile(t.Context(), connectortargets.UpdateCredentialProfileInput{
		TargetID: target.ID, ProfileID: profileID, ConnectorKind: "postgres", Kind: "username_password", Label: "reader",
		Public: map[string]any{
			"username": "reader", "managed_by_aipermission": true, "managed_role_name": "reader",
			"managed_admin_profile_id": 3, "managed_scope": map[string]any{"preset": "read_only"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, combined := range []bool{false, true} {
		if code := update(map[string]any{
			"username": "reader", "managed_by_aipermission": false, "managed_role_name": "replacement",
			"managed_admin_profile_id": 8, "managed_scope": nil, "managed_role_marker": "forged",
		}, combined); code != http.StatusOK {
			t.Fatalf("managed edit failed: combined=%v code=%d", combined, code)
		}
		profile, err := store.GetCredentialProfile(t.Context(), target.ID, profileID)
		if err != nil || !reflect.DeepEqual(profile.Public, seeded.Public) {
			t.Fatalf("managed edit changed recorded identity or added absent marker: %#v err=%v", profile.Public, err)
		}
		if code := update(map[string]any{"username": " reader"}, combined); code != http.StatusBadRequest {
			t.Fatalf("changed exact username accepted: combined=%v code=%d", combined, code)
		}
		profile, err = store.GetCredentialProfile(t.Context(), target.ID, profileID)
		if err != nil || !reflect.DeepEqual(profile.Public, seeded.Public) {
			t.Fatalf("rejected edit changed persisted identity: %#v err=%v", profile.Public, err)
		}
	}
}
