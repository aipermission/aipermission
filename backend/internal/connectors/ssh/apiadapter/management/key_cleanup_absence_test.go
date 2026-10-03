package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
)

func TestAuthenticatedConfirmedAbsenceStillArchivesTarget(t *testing.T) {
	fixture := newCleanupFixture(t)
	server := startCleanupSSHServer(t, fixture, "operator")
	server.absentBeforeCommand.Store(true)
	response, err := fixture.delete(t)
	if err != nil || response.Code != http.StatusOK || server.commands.Load() != 1 || fixture.runtime.keys.secretReads != 1 {
		t.Fatalf("confirmed absence rejected: %d %s err=%v", response.Code, response.Body.String(), err)
	}
	assertConfirmedZeroCleanup(t, fixture, response)
}

func assertConfirmedZeroCleanup(t *testing.T, fixture *cleanupFixture, response *httptest.ResponseRecorder) {
	t.Helper()
	var result struct {
		OK                bool  `json:"ok"`
		RemoteKeyRemoved  bool  `json:"remote_key_removed"`
		RemoteKeysRemoved int64 `json:"remote_keys_removed"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.RemoteKeyRemoved || result.RemoteKeysRemoved != 0 || fixture.gateway.deleteCalls < 1 || fixture.gateway.finalizeCalls < 1 {
		t.Fatalf("zero removal did not complete archival: response=%#v gateway=%#v", result, fixture.gateway)
	}
	entries, err := keycleanup.New(fixture.runtime.journal).List(t.Context())
	if err != nil || len(entries) != 1 || entries[0].Record.Status != keycleanup.Confirmed {
		t.Fatalf("zero removal lacked durable confirmation: %#v err=%v", entries, err)
	}
}
