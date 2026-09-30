package management

import (
	"errors"
	"net/http"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestKeyCleanupInvalidPublicSnapshotNeverPersistsOrReadsPrivateMaterial(t *testing.T) {
	for _, invalid := range []string{"no_profiles", "missing_key", "invalid_key", "missing_revision", "public_encoding", "config_encoding"} {
		t.Run(invalid, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			server := startCleanupSSHServer(t, fixture, "operator")
			switch invalid {
			case "no_profiles":
				fixture.runtime.profiles = nil
			case "missing_key":
				fixture.runtime.profiles[0].Public["ssh_key_id"] = int64(999)
			case "invalid_key":
				_, err := fixture.runtime.keys.Update(t.Context(), fixture.key.ID, connectorapi.UpdateCredentialResourceInput{Name: fixture.key.Name, PublicData: "not an SSH public key"})
				if err != nil {
					t.Fatal(err)
				}
			case "missing_revision":
				fixture.runtime.profiles[0].SecretRevision = ""
			case "public_encoding":
				fixture.runtime.profiles[0].Public["invalid"] = func() {}
			case "config_encoding":
				fixture.target.Config["invalid"] = func() {}
			}
			response, err := fixture.delete(t)
			entries, listErr := keycleanup.New(fixture.runtime.journal).List(t.Context())
			if err != nil || response.Code != http.StatusBadRequest || listErr != nil || len(entries) != 0 || fixture.runtime.keys.secretReads != 0 || server.authAttempts.Load() != 0 {
				t.Fatalf("invalid %s changed remote/local state: %d %s %v %#v %v", invalid, response.Code, response.Body.String(), err, entries, listErr)
			}
		})
	}
}

func TestKeyCleanupInvalidPrivateMaterialRetainsIntentWithoutAuthentication(t *testing.T) {
	for _, material := range []string{"invalid", "mismatched"} {
		t.Run(material, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			server := startCleanupSSHServer(t, fixture, "operator")
			private := "invalid private key"
			if material == "mismatched" {
				store := sshkeys.NewResourceStore(fixture.runtime.keys)
				other, err := store.Create(t.Context(), sshkeys.CreateRequest{Name: "different-key", KeyType: sshkeys.TypeED25519})
				if err != nil {
					t.Fatal(err)
				}
				key, err := store.GetPrivateKey(t.Context(), other.ID)
				if err != nil {
					t.Fatal(err)
				}
				private = key.PrivateKey
			}
			alias, err := fixture.runtime.keys.Create(t.Context(), connectorapi.CreateCredentialResourceInput{
				Name: "mismatched-resource", ResourceType: "ed25519", PublicData: fixture.key.PublicKey, Fingerprint: fixture.key.Fingerprint,
				Secret: map[string]string{"private_key": private},
			})
			if err != nil {
				t.Fatal(err)
			}
			fixture.runtime.profiles[0].Public["ssh_key_id"] = alias.ID
			fixture.runtime.keys.secretReads = 0
			response, err := fixture.delete(t)
			entries, listErr := keycleanup.New(fixture.runtime.journal).List(t.Context())
			if err != nil || response.Code != http.StatusConflict || listErr != nil || len(entries) != 1 || entries[0].Record.Status != keycleanup.Intent || fixture.runtime.keys.secretReads != 1 || server.authAttempts.Load() != 0 {
				t.Fatalf("invalid material authenticated: %d %s %v %#v %v", response.Code, response.Body.String(), err, entries, listErr)
			}
			if _, _, err := keycleanup.New(fixture.runtime.journal).Begin(t.Context(), entries[0].Record.Identity); !errors.Is(err, keycleanup.ErrReconciliationRequired) {
				t.Fatalf("invalid private material lost its uncertainty fence: %v", err)
			}
		})
	}
}
