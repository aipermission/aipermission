package postgresconnector

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/jackc/pgx/v5/pgproto3"
)

type cleanupWireFixture struct {
	client, server net.Conn
	rollback       chan struct{}
	done           chan error
	release        chan struct{}
	mode           string
}

func newCleanupWireFixture(t *testing.T, mode string) *cleanupWireFixture {
	t.Helper()
	client, server := net.Pipe()
	fixture := &cleanupWireFixture{client: client, server: server, rollback: make(chan struct{}), done: make(chan error, 1), release: make(chan struct{}), mode: mode}
	t.Cleanup(func() { close(fixture.release); client.Close(); server.Close(); <-fixture.done })
	go func() {
		defer close(fixture.done)
		defer server.Close()
		fixture.done <- fixture.serve()
	}()
	return fixture
}

func (*cleanupWireFixture) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (fixture *cleanupWireFixture) DialConnectorTCP(context.Context, connectors.NetworkDialRequest) (net.Conn, error) {
	return fixture.client, nil
}

func (fixture *cleanupWireFixture) serve() error {
	backend := pgproto3.NewBackend(fixture.server, fixture.server)
	if _, err := backend.ReceiveStartupMessage(); err != nil {
		return err
	}
	backend.Send(&pgproto3.AuthenticationOk{})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := backend.Flush(); err != nil {
		return err
	}
	status := byte('I')
	var query string
	executed := false
	for {
		message, err := backend.Receive()
		if err != nil {
			return err
		}
		switch message := message.(type) {
		case *pgproto3.Query:
			if strings.EqualFold(strings.TrimSpace(message.String), "rollback") {
				close(fixture.rollback)
				// Consume no more input and send no rollback reply. Client close
				// must unblock this read; no peer timeout can rescue the test.
				var one [1]byte
				_, err := fixture.server.Read(one[:])
				return err
			}
			if strings.HasPrefix(strings.ToLower(message.String), "begin") {
				status = 'T'
				backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("BEGIN")})
			} else {
				backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
			}
			backend.Send(&pgproto3.ReadyForQuery{TxStatus: status})
		case *pgproto3.Parse:
			query = message.Query
			executed = false
			if strings.Contains(message.Query, "pg_namespace") {
				status = 'E'
				backend.Send(&pgproto3.ErrorResponse{Severity: "ERROR", Code: "42P01", Message: "fixture metadata query failed"})
			} else {
				backend.Send(&pgproto3.ParseComplete{})
			}
		case *pgproto3.Describe:
			if status != 'E' {
				parameters := []uint32{}
				if strings.Contains(query, "$1") {
					parameters = []uint32{25}
				}
				backend.Send(&pgproto3.ParameterDescription{ParameterOIDs: parameters})
				field := pgproto3.FieldDescription{Name: []byte("set_config"), DataTypeOID: 25, DataTypeSize: -1, TypeModifier: -1}
				if query == "select 1" {
					field.DataTypeOID, field.DataTypeSize = 23, 4
				}
				backend.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{field}})
			}
		case *pgproto3.Bind:
			backend.Send(&pgproto3.BindComplete{})
		case *pgproto3.Execute:
			executed = true
			if query == "select 1" {
				backend.Send(&pgproto3.DataRow{Values: [][]byte{{0, 0, 0, 1}}})
			}
			backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
		case *pgproto3.Sync:
			backend.Send(&pgproto3.ReadyForQuery{TxStatus: status})
		case *pgproto3.Close:
			backend.Send(&pgproto3.CloseComplete{})
		default:
			return errors.New("unexpected PostgreSQL cleanup fixture message")
		}
		if err := backend.Flush(); err != nil {
			return err
		}
		if _, synced := message.(*pgproto3.Sync); synced && executed && query == "select 1" && fixture.mode == "silent-close" {
			// Stop reading before Terminate. Connection close must use its own
			// deadline rather than relying on a cooperative peer.
			<-fixture.release
			return nil
		}
	}
}
