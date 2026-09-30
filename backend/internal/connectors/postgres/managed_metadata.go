package postgresconnector

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

var _ connectors.CredentialProfileValidator = Connector{}

func (Connector) PreserveProvisionedCredentialPublic(existing connectors.CredentialProfileView, requested map[string]any) (map[string]any, error) {
	next := clonePublicMap(requested)
	// The whole namespace is connector-owned, including fields absent from an old record.
	for name := range next {
		if strings.HasPrefix(name, "managed_") {
			delete(next, name)
		}
	}
	if !boolPublic(existing.Public, "managed_by_aipermission") {
		if requested["managed_by_aipermission"] == false {
			next["managed_by_aipermission"] = false
		}
		return next, nil
	}
	username, ok := existing.Public["username"].(string)
	if !ok || username == "" {
		return nil, fmt.Errorf("managed credential profile is missing its recorded username")
	}
	if value, present := requested["username"]; present && value != username {
		return nil, fmt.Errorf("managed credential username cannot be changed; create a new managed profile instead")
	}
	next["username"] = username
	for name, value := range existing.Public {
		if strings.HasPrefix(name, "managed_") {
			next[name] = value
		}
	}
	return next, nil
}

func (connector Connector) ValidateCredentialProfile(kind string, public, _ map[string]any, previous *connectors.CredentialProfileView) error {
	if kind != "username_password" {
		return fmt.Errorf("unsupported Postgres credential kind")
	}
	existing := connectors.CredentialProfileView{}
	if previous != nil {
		existing = *previous
	}
	expected, err := connector.PreserveProvisionedCredentialPublic(existing, public)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, public) {
		return fmt.Errorf("managed credential metadata is assigned only by connector provisioning and cannot be edited")
	}
	return nil
}

func (Connector) ProvisionedCredentialAdminProfileID(profile connectors.CredentialProfileView) (int64, bool, error) {
	if !boolPublic(profile.Public, "managed_by_aipermission") {
		return 0, false, nil
	}
	adminProfileID := int64Public(profile.Public, "managed_admin_profile_id")
	if adminProfileID < 1 || adminProfileID == profile.ID {
		return 0, true, fmt.Errorf("managed credential profile is missing a valid admin profile reference")
	}
	return adminProfileID, true, nil
}
