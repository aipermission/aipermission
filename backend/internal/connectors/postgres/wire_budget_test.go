package postgresconnector

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/jackc/pgx/v5/pgproto3"
)

type wireCountingConn struct {
	net.Conn
	read atomic.Int64
}

func (c *wireCountingConn) Read(p []byte) (int, error) {
	n, e := c.Conn.Read(p)
	c.read.Add(int64(n))
	return n, e
}

type wireTestTransport struct{ conn net.Conn }

func (wireTestTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}
func (tr wireTestTransport) DialConnectorTCP(context.Context, connectors.NetworkDialRequest) (net.Conn, error) {
	return tr.conn, nil
}

func TestPostgresWireBudget(t *testing.T) {
	for _, size := range []int{1 << 20, maxProtocolMessageBytes - 6, maxProtocolMessageBytes, 8 << 20} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			client, server := net.Pipe()
			counted := &wireCountingConn{Conn: client}
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			defer client.Close()
			defer server.Close()
			done := make(chan error, 1)
			go func() { done <- serveWireBudgetCell(server, size) }()
			runtime := cleanupFixtureRuntime(wireTestTransport{conn: counted})
			prepared, err := New().PrepareAction(ctx, connectors.ActionRequest{
				Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionQueryReadonly,
				Input: map[string]any{"sql": "SELECT repeat('x', 8388608)", "max_rows": 1},
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := New().ExecuteAction(ctx, runtime, prepared)
			if size+6 > maxProtocolMessageBytes {
				var limitErr *pgproto3.ExceededMaxBodyLenErr
				if !errors.As(err, &limitErr) {
					t.Fatalf("expected protocol cap, got %v", err)
				}
				if counted.read.Load() > 8192 {
					t.Fatalf("oversized body was read: %d", counted.read.Load())
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(result.Output)
				if err != nil {
					t.Fatal(err)
				}
				if result.Status != connectors.ResultCompleted || len(encoded) > maxOutputBytes {
					t.Fatalf("bounded valid row failed: status=%s bytes=%d", result.Status, len(encoded))
				}
			}
			_ = client.Close()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("fixture did not finish")
			}
		})
	}
}

func TestPostgresRejectsOversizedStartupMessagesBeforeReadingBodies(t *testing.T) {
	for _, kind := range []byte{'R', 'S', 'N', 'E'} {
		t.Run(string(kind), func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			counted := &wireCountingConn{Conn: client}
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				backend := pgproto3.NewBackend(server, server)
				if _, err := backend.ReceiveStartupMessage(); err != nil {
					done <- err
					return
				}
				var header [5]byte
				header[0] = kind
				binary.BigEndian.PutUint32(header[1:], maxProtocolMessageBytes+5)
				_, err := server.Write(header[:])
				done <- err
			}()
			_, err := connect(ctx, cleanupFixtureRuntime(wireTestTransport{conn: counted}))
			var limitErr *pgproto3.ExceededMaxBodyLenErr
			if !errors.As(err, &limitErr) || counted.read.Load() > 8192 {
				t.Fatalf("startup message %c: bytes=%d err=%v", kind, counted.read.Load(), err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("startup fixture did not finish")
			}
		})
	}
}

func serveWireBudgetCell(conn net.Conn, size int) error {
	b := pgproto3.NewBackend(conn, conn)
	if _, err := b.ReceiveStartupMessage(); err != nil {
		return err
	}
	b.Send(&pgproto3.AuthenticationOk{})
	b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := b.Flush(); err != nil {
		return err
	}
	state := byte('I')
	query := ""
	for {
		msg, err := b.Receive()
		if err != nil {
			return err
		}
		switch m := msg.(type) {
		case *pgproto3.Query:
			if strings.HasPrefix(strings.ToLower(m.String), "begin") {
				state = 'T'
			}
			if strings.EqualFold(strings.TrimSpace(m.String), "commit") || strings.EqualFold(strings.TrimSpace(m.String), "rollback") {
				state = 'I'
			}
			b.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
			b.Send(&pgproto3.ReadyForQuery{TxStatus: state})
		case *pgproto3.Parse:
			query = m.Query
			b.Send(&pgproto3.ParseComplete{})
		case *pgproto3.Describe:
			params := []uint32{}
			if strings.Contains(query, "$1") {
				params = []uint32{25}
			}
			b.Send(&pgproto3.ParameterDescription{ParameterOIDs: params})
			field := pgproto3.FieldDescription{Name: []byte("value"), DataTypeOID: 25, DataTypeSize: -1, TypeModifier: -1}
			if strings.Contains(query, "pg_catalog.pg_cast") {
				field.DataTypeOID = 16
				field.DataTypeSize = 1
			}
			b.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{field}})
		case *pgproto3.Bind:
			b.Send(&pgproto3.BindComplete{})
		case *pgproto3.Execute:
			if strings.Contains(query, "pg_catalog.pg_cast") {
				b.Send(&pgproto3.DataRow{Values: [][]byte{{0}}})
			}
			if strings.Contains(query, "repeat(") {
				if size+6 > maxProtocolMessageBytes {
					if err := b.Flush(); err != nil {
						return err
					}
					var header [5]byte
					header[0] = 'D'
					binary.BigEndian.PutUint32(header[1:], uint32(size+10))
					if _, err := conn.Write(header[:]); err != nil {
						return err
					}
					// No body is sent: rejection must happen from the length header alone.
					var closed [1]byte
					_, err := conn.Read(closed[:])
					return err
				}
				b.Send(&pgproto3.DataRow{Values: [][]byte{[]byte(strings.Repeat("x", size))}})
			}
			b.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
		case *pgproto3.Sync:
			b.Send(&pgproto3.ReadyForQuery{TxStatus: state})
		case *pgproto3.Close:
			b.Send(&pgproto3.CloseComplete{})
		case *pgproto3.Terminate:
			return nil
		}
		if err := b.Flush(); err != nil {
			return err
		}
	}
}
