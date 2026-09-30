package management

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
)

func TestKeyCleanupCancellationAfterRemoteChangePreservesIntentAndBlocksRetry(t *testing.T) {
	fixture := newCleanupFixture(t)
	server := startCleanupSSHServer(t, fixture, "operator")
	server.blockReply.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var status int
	var err error
	go func() {
		defer close(done)
		response, resultErr := fixture.deleteContext(ctx)
		status, err = response.Code, resultErr
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("canceled cleanup worker did not stop")
		}
	})
	select {
	case <-server.executed:
	case <-time.After(5 * time.Second):
		t.Fatal("real remote cleanup did not execute")
	}
	data, readErr := os.ReadFile(filepath.Join(server.homes["operator"], ".ssh", "authorized_keys"))
	if readErr != nil || strings.Contains(string(data), publicKeyBlob(fixture.key.PublicKey)) {
		t.Fatalf("cancellation barrier preceded real revocation: %q %v", data, readErr)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("remote cleanup did not return after cancellation")
	}
	if err != nil || status != http.StatusConflict || fixture.gateway.deleteCalls != 0 {
		t.Fatalf("canceled cleanup reported success: %d %v", status, err)
	}
	entries, err := keycleanup.New(fixture.runtime.journal).List(t.Context())
	if err != nil || len(entries) != 1 || entries[0].Record.Status != keycleanup.Intent {
		t.Fatalf("cancellation evidence: %#v %v", entries, err)
	}
	reads, attempts := fixture.runtime.keys.secretReads, server.authAttempts.Load()
	response, err := fixture.delete(t)
	if err != nil || response.Code != http.StatusConflict || fixture.runtime.keys.secretReads != reads || server.authAttempts.Load() != attempts {
		t.Fatalf("canceled retry reopened transport: %d %s %v", response.Code, response.Body.String(), err)
	}
}
