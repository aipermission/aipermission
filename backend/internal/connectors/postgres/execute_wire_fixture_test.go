package postgresconnector

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/jackc/pgx/v5/pgproto3"
)

type executeWireFixture struct {
	client, server net.Conn
	sql            string
	reply          func(*pgproto3.Backend) (byte, error)
	rejectAuth     bool
	silentRollback bool
	started        chan struct{}
	rollback       chan struct{}
	done           chan struct{}
	err            error
	dials          atomic.Int32
	scripts        int
	simple         []string
	extended       []string
	timeouts       []string
}

func newExecuteWireFixture(t *testing.T, sql string, reply func(*pgproto3.Backend) (byte, error)) *executeWireFixture {
	t.Helper()
	client, server := net.Pipe()
	f := &executeWireFixture{client: client, server: server, sql: sql, reply: reply,
		started: make(chan struct{}), rollback: make(chan struct{}), done: make(chan struct{})}
	t.Cleanup(func() { client.Close(); server.Close(); <-f.done })
	return f
}

func (f *executeWireFixture) start() {
	go func() {
		defer close(f.done)
		defer f.server.Close()
		f.err = f.serve()
	}()
}

func (f *executeWireFixture) wait(t *testing.T) {
	t.Helper()
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		t.Fatal("SQL protocol peer did not close")
	}
}

func (*executeWireFixture) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (f *executeWireFixture) DialConnectorTCP(context.Context, connectors.NetworkDialRequest) (net.Conn, error) {
	if f.dials.Add(1) != 1 {
		return nil, fmt.Errorf("unexpected second SQL connection attempt")
	}
	return f.client, nil
}

func (f *executeWireFixture) serve() error {
	b := pgproto3.NewBackend(f.server, f.server)
	if _, err := b.ReceiveStartupMessage(); err != nil {
		return err
	}
	if f.rejectAuth {
		b.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: "28P01", Message: "fixture authentication denied"})
		return b.Flush()
	}
	b.Send(&pgproto3.AuthenticationOk{})
	b.Send(&pgproto3.BackendKeyData{ProcessID: 42, SecretKey: []byte{1, 2, 3, 4}})
	b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := b.Flush(); err != nil {
		return err
	}
	status, query := byte('I'), ""
	for {
		message, err := b.Receive()
		if err != nil {
			return err
		}
		switch m := message.(type) {
		case *pgproto3.Query:
			status, err = f.simpleQuery(b, m.String, status)
			if err != nil {
				return err
			}
		case *pgproto3.Parse:
			query = m.Query
			f.extended = append(f.extended, query)
			b.Send(&pgproto3.ParseComplete{})
		case *pgproto3.Describe:
			params := []uint32{}
			if strings.Contains(query, "$1") {
				params = []uint32{25}
			}
			b.Send(&pgproto3.ParameterDescription{ParameterOIDs: params})
			field := executeTextField("value")
			if strings.Contains(query, "pg_catalog.pg_cast") {
				field.DataTypeOID, field.DataTypeSize = 16, 1
			}
			b.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{field}})
		case *pgproto3.Bind:
			if strings.Contains(query, "statement_timeout") && len(m.Parameters) == 1 {
				f.timeouts = append(f.timeouts, string(m.Parameters[0]))
			}
			b.Send(&pgproto3.BindComplete{})
		case *pgproto3.Execute:
			value := []byte("fixture")
			if strings.Contains(query, "pg_catalog.pg_cast") {
				value = []byte{0}
			}
			b.Send(&pgproto3.DataRow{Values: [][]byte{value}})
			b.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
		case *pgproto3.Sync:
			b.Send(&pgproto3.ReadyForQuery{TxStatus: status})
		case *pgproto3.Close:
			b.Send(&pgproto3.CloseComplete{})
		case *pgproto3.Terminate:
			return nil
		default:
			return fmt.Errorf("unexpected SQL fixture message %T", message)
		}
		if err := b.Flush(); err != nil {
			return err
		}
	}
}

func (f *executeWireFixture) simpleQuery(b *pgproto3.Backend, sql string, status byte) (byte, error) {
	f.simple = append(f.simple, sql)
	if sql == f.sql {
		f.scripts++
		if f.scripts == 1 {
			close(f.started)
		}
		status, err := f.reply(b)
		if err != nil {
			return status, err
		}
		b.Send(&pgproto3.ReadyForQuery{TxStatus: status})
		return status, nil
	}
	command := "SELECT 1"
	switch strings.ToLower(strings.TrimSpace(sql)) {
	case "begin read only":
		status, command = 'T', "BEGIN"
	case "commit":
		status, command = 'I', "COMMIT"
	case "rollback":
		close(f.rollback)
		if f.silentRollback {
			_, err := b.Receive()
			return status, err
		}
		status, command = 'I', "ROLLBACK"
	}
	b.Send(&pgproto3.CommandComplete{CommandTag: []byte(command)})
	b.Send(&pgproto3.ReadyForQuery{TxStatus: status})
	return status, nil
}

func executeTextField(name string) pgproto3.FieldDescription {
	return pgproto3.FieldDescription{Name: []byte(name), DataTypeOID: 25, DataTypeSize: -1, TypeModifier: -1}
}

func sendExecuteRows(b *pgproto3.Backend, fields []pgproto3.FieldDescription, values ...[][]byte) {
	b.Send(&pgproto3.RowDescription{Fields: fields})
	for _, row := range values {
		b.Send(&pgproto3.DataRow{Values: row})
	}
	b.Send(&pgproto3.CommandComplete{CommandTag: []byte(fmt.Sprintf("SELECT %d", len(values)))})
}

func prepareExecuteFixture(t *testing.T, runtime connectors.RuntimeContext, name string, input map[string]any) connectors.PreparedAction {
	t.Helper()
	action, err := New().PrepareAction(t.Context(), connectors.ActionRequest{
		Target: runtime.Target, Profile: runtime.Profile, ActionName: name, Input: input,
	})
	if err != nil {
		t.Fatal(err)
	}
	return action
}
