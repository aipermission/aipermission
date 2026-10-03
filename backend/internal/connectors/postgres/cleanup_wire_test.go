package postgresconnector

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestQueryFailureBoundsSilentRollbackAndClosesSocket(t *testing.T) {
	for _, cancelMode := range []string{"cancel", "deadline"} {
		t.Run(cancelMode, func(t *testing.T) { assertSilentRollbackBounded(t, cancelMode) })
	}
}

func assertSilentRollbackBounded(t *testing.T, cancelMode string) {
	t.Helper()
	fixture := newCleanupWireFixture(t, "silent-rollback")
	ctx, cancel := context.WithCancel(t.Context())
	if cancelMode == "deadline" {
		cancel()
		ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	}
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := New().ExecuteAction(ctx, cleanupFixtureRuntime(fixture), connectors.PreparedAction{ActionName: ActionGetSchemas})
		result <- err
	}()
	select {
	case <-fixture.rollback:
	case err := <-result:
		t.Fatalf("action returned before rollback: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("query failure did not reach rollback")
	}
	if cancelMode == "cancel" {
		cancel()
	} else {
		<-ctx.Done()
	}
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "fixture metadata query failed") || errors.Is(err, context.Canceled) {
			t.Fatalf("bounded cleanup lost the definite original query error: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("silent rollback retained the action after its cleanup budget")
	}
	select {
	case <-fixture.done:
	case <-time.After(time.Second):
		t.Fatal("bounded rollback did not close the underlying socket and join its peer")
	}
}

func TestConnectionSuccessBoundsSilentClose(t *testing.T) {
	fixture := newCleanupWireFixture(t, "silent-close")
	result := make(chan connectors.TestResult, 1)
	go func() {
		value, _ := New().TestConnection(t.Context(), cleanupFixtureRuntime(fixture))
		result <- value
	}()
	select {
	case value := <-result:
		if value.Status != connectors.TestOK {
			t.Fatalf("successful query result changed by close failure: %#v", value)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("silent peer retained connection-test completion")
	}
	if _, err := fixture.client.Write(nil); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("connection socket remains open: %v", err)
	}
}

func cleanupFixtureRuntime(transport connectors.NetworkTransport) connectors.RuntimeContext {
	return connectors.RuntimeContext{
		Target:  connectors.TargetView{ConnectorKind: Kind, Config: map[string]any{"host": "127.0.0.1", "port": 5432, "database": "fixture", "ssl_mode": "disable"}},
		Profile: connectors.CredentialProfileView{Public: map[string]any{"username": "fixture"}},
		Secrets: fakeSecrets{"password": "fixture-only"}, Capabilities: fakePostgresCapabilities{transport: transport},
	}
}
