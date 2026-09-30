package postgresconnector

import (
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestManagedMetadataPreservesStoredNamespaceAndAbsence(t *testing.T) {
	existing := connectors.CredentialProfileView{Public: map[string]any{
		"username": "reader", "managed_by_aipermission": true,
		"managed_role_name": "reader", "managed_admin_profile_id": int64(3),
		"managed_scope":    map[string]any{"preset": "read_only"},
		"managed_identity": "recorded-public-identity",
	}}
	requested := map[string]any{
		"username": "reader", "managed_by_aipermission": false,
		"managed_role_name": "replacement", "managed_admin_profile_id": int64(8),
		"managed_scope": nil, "managed_identity": "forged-identity",
		"managed_role_marker": "forged-absent-marker", "managed_future_field": "unrecorded",
	}
	before := clonePublicMap(requested)
	next, err := New().PreserveProvisionedCredentialPublic(existing, requested)
	if err != nil || !reflect.DeepEqual(next, existing.Public) {
		t.Fatalf("managed metadata drifted: next=%#v err=%v", next, err)
	}
	if !reflect.DeepEqual(requested, before) {
		t.Fatal("preservation mutated the caller's input")
	}
	next["username"] = "changed-local-copy"
	if existing.Public["username"] != "reader" || requested["username"] != "reader" {
		t.Fatal("preservation returned the original public map")
	}
}

func TestManagedMetadataCannotPromoteAnUnmanagedProfile(t *testing.T) {
	for _, public := range []map[string]any{nil, {"username": "reader"}, {"username": "reader", "managed_by_aipermission": false}} {
		for _, flag := range []any{true, "true", nil, false} {
			requested := map[string]any{
				"username": "other-reader", "managed_by_aipermission": flag,
				"managed_role_name": "replacement", "managed_admin_profile_id": int64(3),
				"managed_identity": "forged-identity",
			}
			next, err := New().PreserveProvisionedCredentialPublic(connectors.CredentialProfileView{Public: public}, requested)
			if err != nil || next["username"] != "other-reader" {
				t.Fatalf("normal credential edit: %#v err=%v", next, err)
			}
			for key, value := range next {
				if key != "username" && (key != "managed_by_aipermission" || value != false) {
					t.Fatalf("unmanaged profile accepted reserved field %q=%#v", key, value)
				}
			}
		}
	}
}

func TestManagedMetadataUsesExactUsernameIdentity(t *testing.T) {
	existing := connectors.CredentialProfileView{Public: map[string]any{"username": "reader", "managed_by_aipermission": true}}
	for _, username := range []any{" reader", "reader ", "other", nil, 12} {
		if _, err := New().PreserveProvisionedCredentialPublic(existing, map[string]any{"username": username}); err == nil {
			t.Fatalf("accepted changed username identity %#v", username)
		}
	}
	for _, requested := range []map[string]any{nil, {}, {"username": "reader"}} {
		next, err := New().PreserveProvisionedCredentialPublic(existing, requested)
		if err != nil || next["username"] != "reader" {
			t.Fatalf("unchanged or omitted username: %#v err=%v", next, err)
		}
	}
	for _, username := range []any{nil, "", 12} {
		existing.Public["username"] = username
		if _, err := New().PreserveProvisionedCredentialPublic(existing, nil); err == nil {
			t.Fatalf("malformed managed username %#v was accepted", username)
		}
	}
}

func TestManagedMetadataValidationUsesGenericCredentialPreparation(t *testing.T) {
	connector := New()
	for _, reserved := range []map[string]any{
		{"managed_by_aipermission": true}, {"managed_role_name": "reader"},
		{"managed_admin_profile_id": int64(3)}, {"managed_admin_profile_ref": "admin"},
		{"managed_preset": "read_only"}, {"managed_scope": map[string]any{"preset": "read_only"}},
	} {
		public := clonePublicMap(reserved)
		public["username"] = "reader"
		_, err := connectormanagement.PrepareCredentialProfile(t.Context(), connector, connectormanagement.CredentialProfileInput{
			Kind: "username_password", Label: "My credential", Public: public,
			Secret: map[string]any{"password": "unit-test-only"},
		}, true, nil, "", connectormanagement.CredentialPreparationPorts{})
		if err == nil {
			t.Fatalf("generic create accepted forged management metadata %#v", reserved)
		}
	}
	for _, public := range []map[string]any{{"username": "reader"}, {"username": "reader", "managed_by_aipermission": false}} {
		if _, err := connectormanagement.PrepareCredentialProfile(t.Context(), connector, connectormanagement.CredentialProfileInput{
			Kind: "username_password", Label: "My credential", Public: public,
			Secret: map[string]any{"password": "unit-test-only"},
		}, true, nil, "", connectormanagement.CredentialPreparationPorts{}); err != nil {
			t.Fatalf("ordinary credential was rejected: %v", err)
		}
	}
}

func TestManagedMetadataValidatorRejectsBypassedPreservation(t *testing.T) {
	connector := New()
	validator, ok := any(connector).(connectors.CredentialProfileValidator)
	if !ok {
		t.Fatal("connector does not declare the generic credential validator")
	}
	existing := connectors.CredentialProfileView{Public: map[string]any{
		"username": "reader", "managed_by_aipermission": true, "managed_role_name": "reader",
	}}
	for _, public := range []map[string]any{
		{"username": "reader"},
		{"username": "replacement", "managed_by_aipermission": true, "managed_role_name": "reader"},
		{"username": "reader", "managed_by_aipermission": true, "managed_role_name": "reader", "managed_role_marker": "forged"},
	} {
		if err := validator.ValidateCredentialProfile("username_password", public, nil, &existing); err == nil {
			t.Fatalf("direct validator accepted ownership drift %#v", public)
		}
	}
	if err := validator.ValidateCredentialProfile("username_password", clonePublicMap(existing.Public), nil, &existing); err != nil {
		t.Fatalf("unchanged managed metadata rejected: %v", err)
	}
	if err := validator.ValidateCredentialProfile("unsupported", map[string]any{"username": "reader"}, nil, nil); err == nil {
		t.Fatal("direct validator accepted an unsupported credential kind")
	}
}

func TestManagedMetadataAdminReferenceRemainsFailClosed(t *testing.T) {
	for _, profile := range []connectors.CredentialProfileView{{}, {Public: map[string]any{"managed_by_aipermission": false}}} {
		id, managed, err := New().ProvisionedCredentialAdminProfileID(profile)
		if id != 0 || managed || err != nil {
			t.Fatalf("ordinary profile required cleanup: id=%d managed=%v err=%v", id, managed, err)
		}
	}
	for _, id := range []int64{0, -1, 7} {
		_, managed, err := New().ProvisionedCredentialAdminProfileID(connectors.CredentialProfileView{
			ID: 7, Public: map[string]any{"managed_by_aipermission": true, "managed_admin_profile_id": id},
		})
		if !managed || err == nil {
			t.Fatalf("invalid admin reference %d accepted", id)
		}
	}
	id, managed, err := New().ProvisionedCredentialAdminProfileID(connectors.CredentialProfileView{
		ID: 7, Public: map[string]any{"managed_by_aipermission": true, "managed_admin_profile_id": int64(3)},
	})
	if id != 3 || !managed || err != nil {
		t.Fatalf("valid admin reference rejected: id=%d managed=%v err=%v", id, managed, err)
	}
}
