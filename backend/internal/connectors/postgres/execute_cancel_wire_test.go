package postgresconnector

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/jackc/pgx/v5/pgproto3"
)

type cancellationWireFixture struct {
	*executeWireFixture
	canceled chan error
}

func (f *cancellationWireFixture) DialConnectorTCP(context.Context, connectors.NetworkDialRequest) (net.Conn, error) {
	switch f.dials.Add(1) {
	case 1:
		return f.client, nil
	case 2:
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			message, err := pgproto3.NewBackend(server, server).ReceiveStartupMessage()
			if err == nil {
				request, ok := message.(*pgproto3.CancelRequest)
				if !ok || request.ProcessID != 42 || !reflect.DeepEqual(request.SecretKey, []byte{1, 2, 3, 4}) {
					err = fmt.Errorf("invalid native cancellation request")
				}
			}
			f.canceled <- err
		}()
		return noDeadlineExecutionConn{client}, nil
	default:
		return nil, fmt.Errorf("unexpected additional SQL connection")
	}
}

func TestExecuteSQLCancellationDeliversNativeCancelAfterActionExpires(t *testing.T) {
	f := &cancellationWireFixture{
		executeWireFixture: newExecuteWireFixture(t, "SELECT pg_sleep(60)", nil),
		canceled:           make(chan error, 1),
	}
	f.client = noDeadlineExecutionConn{f.client}
	f.reply = func(b *pgproto3.Backend) (byte, error) {
		_, err := b.Receive()
		return 'I', err
	}
	f.start()
	runtime := cleanupFixtureRuntime(f)
	action := prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": f.sql})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan connectors.ActionResult, 1)
	go func() { result, _ := New().ExecuteAction(ctx, runtime, action); done <- result }()
	select {
	case <-f.started:
	case <-time.After(3 * time.Second):
		t.Fatal("SQL was not dispatched")
	}
	cancel()
	select {
	case result := <-done:
		assertExecuteUnknown(t, result, nil)
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not release SQL execution")
	}
	select {
	case err := <-f.canceled:
		if err != nil {
			t.Fatalf("native cancellation was not delivered: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("action expiration prevented the cancellation connection")
	}
	f.wait(t)
	if f.dials.Load() != 2 || f.scripts != 1 {
		t.Fatal("cancellation retried SQL or did not use a separate connection")
	}
}
