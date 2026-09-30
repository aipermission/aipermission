package management

import (
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestKeyCleanupPartialGroupsRemainDurableAndNeverReauthenticateOnRetry(t *testing.T) {
	fixture := newCleanupFixture(t)
	server := startCleanupSSHServer(t, fixture, "operator", "secondary")
	second := fixture.runtime.profiles[0]
	second.ID, second.Public = 3, map[string]any{"username": "secondary", "ssh_key_id": fixture.key.ID}
	fixture.runtime.profiles = append(fixture.runtime.profiles, second)
	fixture.runtime.keys.onSecret = func() {
		entries, err := keycleanup.New(fixture.runtime.journal).List(t.Context())
		if err != nil || len(entries) != 2 {
			t.Fatalf("all intents must precede any private delivery: %#v %v", entries, err)
		}
		if fixture.runtime.keys.secretReads == 2 {
			fixture.runtime.keys.refuseSecret = true
		}
	}
	response, err := fixture.delete(t)
	if err != nil || response.Code != http.StatusConflict || server.commands.Load() != 1 || fixture.gateway.deleteCalls != 0 {
		t.Fatalf("partial cleanup: %d %s %v, commands %d", response.Code, response.Body.String(), err, server.commands.Load())
	}
	entries, err := keycleanup.New(fixture.runtime.journal).List(t.Context())
	if err != nil || len(entries) != 2 {
		t.Fatalf("partial evidence missing: %#v %v", entries, err)
	}
	for _, entry := range entries {
		want := keycleanup.Intent
		if entry.Record.Identity.Username == "operator" {
			want = keycleanup.Confirmed
		}
		if entry.Record.Status != want {
			t.Fatalf("partial status not preserved: %#v", entry)
		}
	}
	reads, attempts := fixture.runtime.keys.secretReads, server.authAttempts.Load()
	fixture.runtime.keys.refuseSecret = false
	response, err = fixture.delete(t)
	if err != nil || response.Code != http.StatusConflict || fixture.runtime.keys.secretReads != reads || server.authAttempts.Load() != attempts {
		t.Fatalf("partial retry authenticated: %d %s %v", response.Code, response.Body.String(), err)
	}
}

func TestKeyCleanupPreservesConfiguredHostnameSpellingForTrustVerification(t *testing.T) {
	fixture := newCleanupFixture(t)
	server := startCleanupSSHServer(t, fixture, "operator")
	_, port, _ := net.SplitHostPort(server.listener.Addr().String())
	fixture.target.Config["host"] = "LOCALHOST"
	line := knownhosts.Line([]string{net.JoinHostPort("LOCALHOST", port)}, server.host.PublicKey()) + "\n"
	if err := os.WriteFile(fixture.gateway.path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	response, err := fixture.delete(t)
	if err != nil || response.Code != http.StatusOK || server.commands.Load() != 1 {
		t.Fatalf("configured trust identity spelling changed: %d %s %v", response.Code, response.Body.String(), err)
	}
	entries, err := keycleanup.New(fixture.runtime.journal).List(t.Context())
	if err != nil || len(entries) != 1 || entries[0].Record.Identity.Host != strings.ToLower("LOCALHOST") {
		t.Fatalf("journal identity is not canonical: %#v %v", entries, err)
	}
}

func TestKeyCleanupGroupsDuplicatePublicMaterialDespiteKeyIDsAndComments(t *testing.T) {
	fixture := newCleanupFixture(t)
	server := startCleanupSSHServer(t, fixture, "operator")
	alias, err := fixture.runtime.keys.Create(t.Context(), connectorapi.CreateCredentialResourceInput{
		Name: "alias", ResourceType: "ed25519", PublicData: fixture.key.PublicKey + " alternative-comment",
		Fingerprint: fixture.key.Fingerprint, Secret: struct{}{},
	})
	if err != nil {
		t.Fatal(err)
	}
	second := fixture.runtime.profiles[0]
	second.ID, second.Public = 3, map[string]any{"username": "operator", "ssh_key_id": alias.ID}
	fixture.runtime.profiles = append(fixture.runtime.profiles, second)
	response, err := fixture.delete(t)
	if err != nil || response.Code != http.StatusOK || server.commands.Load() != 1 || fixture.runtime.keys.secretReads != 1 {
		t.Fatalf("shared material authenticated twice: %d %s %v", response.Code, response.Body.String(), err)
	}
	entries, err := keycleanup.New(fixture.runtime.journal).List(t.Context())
	if err != nil || len(entries) != 1 || len(entries[0].Record.Identity.Profiles) != 2 || entries[0].Record.Identity.Profiles[1].KeyID != alias.ID {
		t.Fatalf("alias identity missing: %#v %v", entries, err)
	}
}

func TestKeyCleanupMalformedJournalAndMissingTrustRejectBeforePrivateKeyDelivery(t *testing.T) {
	for _, cause := range []string{"malformed", "untrusted"} {
		t.Run(cause, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			var server *cleanupSSHServer
			if cause == "malformed" {
				server = startCleanupSSHServer(t, fixture, "operator")
				_, err := fixture.runtime.journal.Create(t.Context(), connectorapi.CreateCredentialResourceInput{Name: "invalid-evidence", ResourceType: "key_revocation.v1", PublicData: "{}", Secret: struct{}{}})
				if err != nil {
					t.Fatal(err)
				}
			}
			response, err := fixture.delete(t)
			want := http.StatusConflict
			if cause == "untrusted" {
				want = http.StatusBadRequest
			}
			if err != nil || response.Code != want || fixture.runtime.keys.secretReads != 0 || fixture.gateway.deleteCalls != 0 || (server != nil && server.authAttempts.Load() != 0) {
				t.Fatalf("unsafe preflight authenticated: %d %s %v", response.Code, response.Body.String(), err)
			}
		})
	}
}
