package management

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestKeyCleanupUsesAllPublicProfilesBeforeSecretAndPersistsConfirmation(t *testing.T) {
	fixture := newCleanupFixture(t)
	server := startCleanupSSHServer(t, fixture, "operator")
	second := fixture.runtime.profiles[0]
	second.ID = 3
	fixture.runtime.profiles = []connectors.CredentialProfileView{second, fixture.runtime.profiles[0]}
	fixture.runtime.keys.onSecret = func() {
		entries, err := keycleanup.New(fixture.runtime.journal).List(t.Context())
		if err != nil || len(entries) != 1 || entries[0].Record.Status != keycleanup.Intent || len(entries[0].Record.Identity.Profiles) != 2 {
			t.Fatalf("secret read before complete durable group: %#v %v", entries, err)
		}
	}
	response, err := fixture.delete(t)
	if err != nil || response.Code != http.StatusOK || fixture.runtime.keys.secretReads != 1 || server.commands.Load() != 1 {
		t.Fatalf("cleanup %d %s, err %v, secrets %d, commands %d", response.Code, response.Body.String(), err, fixture.runtime.keys.secretReads, server.commands.Load())
	}
	entries, err := keycleanup.New(fixture.runtime.journal).List(t.Context())
	if err != nil || len(entries) != 1 || entries[0].Record.Status != keycleanup.Confirmed || entries[0].Record.Identity.Profiles[0].ID != 2 {
		t.Fatalf("confirmation: %#v %v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(server.homes["operator"], ".ssh", "authorized_keys"))
	if err != nil || strings.Contains(string(data), publicKeyBlob(fixture.key.PublicKey)) {
		t.Fatalf("real key not revoked: %q %v", data, err)
	}
	if fixture.runtime.journal.secretReads != 0 || fixture.gateway.deleteCalls != 1 || fixture.gateway.finalizeCalls != 1 {
		t.Fatalf("unexpected lifecycle: %+v %+v", fixture.runtime.journal, fixture.gateway)
	}
}

// Adapter replay retains public snapshots. After successful archival the public
// HTTP delete lookup is not available; pending finalization belongs to core.
func TestKeyCleanupConfirmedEvidenceSurvivesLocalFailureDuringAdapterReplay(t *testing.T) {
	for _, stage := range []string{"delete", "finalize"} {
		t.Run(stage, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			server := startCleanupSSHServer(t, fixture, "operator")
			if stage == "delete" {
				fixture.gateway.deleteErr = errors.New("archive fixture failed")
			} else {
				fixture.gateway.finalizeErr = errors.New("finalization fixture failed")
			}
			response, err := fixture.delete(t)
			if stage == "delete" && response.Code != http.StatusInternalServerError || stage == "finalize" && err == nil {
				t.Fatalf("missing local failure: %d %s %v", response.Code, response.Body.String(), err)
			}
			fixture.gateway.deleteErr, fixture.gateway.finalizeErr = nil, nil
			attempts, reads := server.authAttempts.Load(), fixture.runtime.keys.secretReads
			response, err = fixture.delete(t)
			if err != nil || response.Code != http.StatusOK || attempts != server.authAttempts.Load() || reads != fixture.runtime.keys.secretReads || server.commands.Load() != 1 {
				t.Fatalf("retry borrowed revoked key: %d %s %v attempts %d->%d secrets %d->%d", response.Code, response.Body.String(), err, attempts, server.authAttempts.Load(), reads, fixture.runtime.keys.secretReads)
			}
		})
	}
}

func TestKeyCleanupReplyAndPersistenceAmbiguityNeverReauthenticates(t *testing.T) {
	for _, stage := range []string{"remote_reply", "intent_response", "confirm_commit", "confirm_response"} {
		t.Run(stage, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			server := startCleanupSSHServer(t, fixture, "operator")
			server.loseReply.Store(stage == "remote_reply")
			fixture.runtime.journal.createAfter = stage == "intent_response"
			fixture.runtime.journal.confirmBefore = stage == "confirm_commit"
			fixture.runtime.journal.confirmAfter = stage == "confirm_response"
			response, err := fixture.delete(t)
			if err != nil || response.Code != http.StatusConflict || fixture.gateway.deleteCalls != 0 {
				t.Fatalf("ambiguity removed local record: %d %s %v", response.Code, response.Body.String(), err)
			}
			entries, err := keycleanup.New(fixture.runtime.journal).List(t.Context())
			if err != nil || len(entries) != 1 {
				t.Fatalf("durable evidence missing: %#v %v", entries, err)
			}
			fixture.reopen(t)
			attempts, reads := server.authAttempts.Load(), fixture.runtime.keys.secretReads
			response, err = fixture.delete(t)
			want := http.StatusConflict
			if stage == "confirm_response" {
				want = http.StatusOK
			}
			if err != nil || response.Code != want || server.authAttempts.Load() != attempts || fixture.runtime.keys.secretReads != reads {
				t.Fatalf("uncertain retry dispatched: %d %s %v, attempts %d->%d secrets %d->%d", response.Code, response.Body.String(), err, attempts, server.authAttempts.Load(), reads, fixture.runtime.keys.secretReads)
			}
		})
	}
}

func TestKeyCleanupPreflightsInvalidLaterProfileWithoutAnyIntentOrSecret(t *testing.T) {
	fixture := newCleanupFixture(t)
	server := startCleanupSSHServer(t, fixture, "operator")
	second := fixture.runtime.profiles[0]
	second.ID, second.TargetID = 3, 99
	fixture.runtime.profiles = append(fixture.runtime.profiles, second)
	response, err := fixture.delete(t)
	entries, listErr := keycleanup.New(fixture.runtime.journal).List(t.Context())
	if err != nil || response.Code != http.StatusBadRequest || listErr != nil || len(entries) != 0 || fixture.runtime.keys.secretReads != 0 || server.authAttempts.Load() != 0 {
		t.Fatalf("invalid later profile changed anything: %d %s %v %#v %v", response.Code, response.Body.String(), err, entries, listErr)
	}
}

func TestKeyCleanupConfirmedIdentityDriftStopsBeforeSecret(t *testing.T) {
	fixture := newCleanupFixture(t)
	server := startCleanupSSHServer(t, fixture, "operator")
	response, err := fixture.delete(t)
	if err != nil || response.Code != http.StatusOK {
		t.Fatalf("initial cleanup: %d %s %v", response.Code, response.Body.String(), err)
	}
	fixture.runtime.profiles[0].SecretRevision = "2"
	reads, attempts := fixture.runtime.keys.secretReads, server.authAttempts.Load()
	response, err = fixture.delete(t)
	if err != nil || response.Code != http.StatusConflict || reads != fixture.runtime.keys.secretReads || attempts != server.authAttempts.Load() {
		t.Fatalf("drift reused revoked key: %d %s %v", response.Code, response.Body.String(), err)
	}
}

func TestKeyCleanupPlansSharedMaterialIndependentlyOfProfileOrdering(t *testing.T) {
	fixture := newCleanupFixture(t)
	startCleanupSSHServer(t, fixture, "operator")
	second := fixture.runtime.profiles[0]
	second.ID = 3
	fixture.runtime.profiles = append(fixture.runtime.profiles, second)
	first, err := planKeyCleanup(t.Context(), fixture.gateway, fixture.runtime, fixture.target, fixture.runtime.profiles)
	if err != nil {
		t.Fatal(err)
	}
	fixture.runtime.profiles[0], fixture.runtime.profiles[1] = fixture.runtime.profiles[1], fixture.runtime.profiles[0]
	reordered, err := planKeyCleanup(t.Context(), fixture.gateway, fixture.runtime, fixture.target, fixture.runtime.profiles)
	if err != nil || !reflect.DeepEqual(first, reordered) || len(first) != 1 || fixture.runtime.keys.secretReads != 0 {
		t.Fatalf("noncanonical shared group: %#v %#v %v", first, reordered, err)
	}
}

func TestConfirmedKeyRemovalRequiresExactSuccessfulProtocol(t *testing.T) {
	for _, stdout := range []string{"", "aipermission_key_removed=01\n", "aipermission_key_removed=-1\n", "aipermission_key_removed=1\nextra", "prefix aipermission_key_removed=1", "aipermission_key_removed=18446744073709551616\n"} {
		if _, err := confirmedKeyRemovalCount(execution.Result{Stdout: stdout, DispatchStarted: true}); err == nil {
			t.Fatalf("accepted ambiguous removal output %q", stdout)
		}
	}
	for _, result := range []execution.Result{{Stdout: "aipermission_key_removed=1\n", ExitCode: 1, DispatchStarted: true}, {Stdout: "aipermission_key_removed=1\n", Stderr: "remote key uninstall removed 0 authorized_keys entries", DispatchStarted: true}, {Stdout: "aipermission_key_removed=1\n"}} {
		if _, err := confirmedKeyRemovalCount(result); err == nil {
			t.Fatalf("accepted conflicting removal protocol %#v", result)
		}
	}
	for _, value := range []uint64{0, 1, 2} {
		count, err := confirmedKeyRemovalCount(execution.Result{Stdout: "aipermission_key_removed=" + string(rune('0'+value)) + "\n", DispatchStarted: true})
		if err != nil || count != value {
			t.Fatalf("valid removal protocol: %d %v", count, err)
		}
	}
}

var _ connectorapi.TargetLifecycleRuntime = (*cleanupRuntime)(nil)
var _ connectorapi.TargetDeletionGateway = (*cleanupGateway)(nil)
