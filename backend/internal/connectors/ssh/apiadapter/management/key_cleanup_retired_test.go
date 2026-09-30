package management

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
)

func TestKeyCleanupUnresolvedRetiredGroupFencesDeletionAfterDatabaseReopen(t *testing.T) {
	for _, change := range []string{"username", "key_material", "removed_profile"} {
		t.Run(change, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			server := startCleanupSSHServer(t, fixture, "operator", "secondary")
			groups, err := planKeyCleanup(t.Context(), fixture.gateway, fixture.runtime, fixture.target, fixture.runtime.profiles)
			if err != nil {
				t.Fatal(err)
			}
			entry, dispatch, err := keycleanup.New(fixture.runtime.journal).Begin(t.Context(), groups[0].Identity)
			if err != nil || !dispatch {
				t.Fatalf("seed intent: %#v %v", entry, err)
			}
			fixture.reopen(t)
			retireCleanupProfile(t, fixture, server, change)
			response, err := fixture.delete(t)
			entries, listErr := keycleanup.New(fixture.runtime.journal).List(t.Context())
			if err != nil || response.Code != http.StatusConflict || fixture.gateway.deleteCalls != 0 || fixture.gateway.finalizeCalls != 0 || fixture.runtime.keys.secretReads != 0 || server.authAttempts.Load() != 0 || server.commands.Load() != 0 || listErr != nil || len(entries) != 1 {
				t.Fatalf("retired uncertainty bypassed: response %d %s error %v entries %#v list %v reads %d auth %d archive %d", response.Code, response.Body.String(), err, entries, listErr, fixture.runtime.keys.secretReads, server.authAttempts.Load(), fixture.gateway.deleteCalls)
			}
			if entries[0].Record.Generation != entry.Record.Generation || entries[0].Record.Status != keycleanup.Intent {
				t.Fatalf("retired intent changed: %#v", entries[0])
			}
		})
	}
}

func TestKeyCleanupConfirmedRetiredGroupDoesNotReauthenticate(t *testing.T) {
	for _, change := range []string{"username", "key_material", "removed_profile"} {
		t.Run(change, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			server := startCleanupSSHServer(t, fixture, "operator", "secondary")
			fixture.gateway.deleteErr = errors.New("local archival refused")
			response, err := fixture.delete(t)
			if err != nil || response.Code != http.StatusInternalServerError || server.commands.Load() != 1 {
				t.Fatalf("verified cleanup before failed archive: %d %s %v", response.Code, response.Body.String(), err)
			}
			entries, err := keycleanup.New(fixture.runtime.journal).List(t.Context())
			if err != nil || len(entries) != 1 || entries[0].Record.Status != keycleanup.Confirmed {
				t.Fatalf("durable confirmation missing: %#v %v", entries, err)
			}
			original := entries[0]
			fixture.reopen(t)
			retireCleanupProfile(t, fixture, server, change)
			fixture.gateway.deleteErr = nil
			fixture.gateway.deleteCalls = 0
			response, err = fixture.delete(t)
			if err != nil || response.Code != http.StatusOK || fixture.runtime.keys.secretReads != 1 || server.commands.Load() != 2 || server.authAttempts.Load() != 2 || fixture.gateway.deleteCalls != 1 || fixture.gateway.finalizeCalls != 1 {
				t.Fatalf("confirmed retired group reauthenticated or blocked: %d %s %v reads %d auth %d commands %d", response.Code, response.Body.String(), err, fixture.runtime.keys.secretReads, server.authAttempts.Load(), server.commands.Load())
			}
			entries, err = keycleanup.New(fixture.runtime.journal).List(t.Context())
			if err != nil || len(entries) != 2 {
				t.Fatalf("retired history was not preserved: %#v %v", entries, err)
			}
			found := false
			for _, entry := range entries {
				if entry.Record.Status != keycleanup.Confirmed {
					t.Fatalf("completed retired evidence changed: %#v", entry)
				}
				if entry.ResourceID == original.ResourceID {
					found = true
					if !reflect.DeepEqual(entry, original) {
						t.Fatalf("original retired evidence changed: %#v", entry)
					}
				}
			}
			if !found {
				t.Fatal("original retired evidence disappeared")
			}
		})
	}
}

func retireCleanupProfile(t *testing.T, fixture *cleanupFixture, server *cleanupSSHServer, change string) {
	t.Helper()
	profile := &fixture.runtime.profiles[0]
	switch change {
	case "username":
		profile.Public["username"] = "secondary"
	case "removed_profile":
		profile.ID = 3
		profile.Public["username"] = "secondary"
	case "key_material":
		key, err := sshkeys.NewResourceStore(fixture.runtime.keys).Create(t.Context(), sshkeys.CreateRequest{Name: "replacement", KeyType: sshkeys.TypeED25519})
		if err != nil {
			t.Fatal(err)
		}
		profile.Public["ssh_key_id"] = key.ID
		path := filepath.Join(server.homes["operator"], ".ssh", "authorized_keys")
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(contents, []byte(key.PublicKey+"\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown test change %q", change)
	}
	profile.UpdatedAt = "profile-v2"
}
