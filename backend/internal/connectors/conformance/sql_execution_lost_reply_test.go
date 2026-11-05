package conformance_test

import (
	"context"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

// Discard the first response bytes after an independent connection witnesses
// the mutation commit. TLS and native protocol behavior are otherwise unchanged.
type sqlLostReplyTransport struct {
	lifetime context.Context
	witness  func(context.Context) bool
	seen     atomic.Bool
	dials    atomic.Int32
}

func (*sqlLostReplyTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}
func (d *sqlLostReplyTransport) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.NetworkTransportCapabilityName {
		return d
	}
	return nil
}
func (d *sqlLostReplyTransport) DialConnectorTCP(ctx context.Context, r connectors.NetworkDialRequest) (net.Conn, error) {
	c, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(r.Host, strconv.Itoa(r.Port)))
	if err != nil {
		return nil, err
	}
	if d.dials.Add(1) == 1 {
		return &sqlLostReplyConn{Conn: c, owner: d}, nil
	}
	return c, nil
}

type sqlLostReplyConn struct {
	net.Conn
	owner *sqlLostReplyTransport
}

func (c *sqlLostReplyConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		ctx, cancel := context.WithTimeout(c.owner.lifetime, time.Second)
		seen := c.owner.witness(ctx)
		cancel()
		if seen {
			c.owner.seen.Store(true)
			_ = c.Conn.Close()
			return 0, io.ErrUnexpectedEOF
		}
	}
	return n, err
}

func assertSQLLostCommittedReply(t *testing.T, f sqlExecutionFixture, transport *sqlLostReplyTransport, value func(*testing.T) int) {
	t.Helper()
	f.runtime.Capabilities = transport
	prepared := prepareSQLExecution(t, f, map[string]any{"sql": "UPDATE " + f.table + " SET value=value+37;"})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := f.connector.ExecuteAction(ctx, f.runtime, prepared)
	assertSQLExecutionUnknown(t, result, err)
	if !transport.seen.Load() || value(t) != 37 || transport.dials.Load() > 2 {
		t.Fatalf("lost reply was not witnessed or mutation was replayed: seen=%t dials=%d", transport.seen.Load(), transport.dials.Load())
	}
}

func testPostgresSQLLostReply(t *testing.T) {
	f := newPostgresExecutionFixture(t)
	transport := &sqlLostReplyTransport{lifetime: t.Context(), witness: func(ctx context.Context) bool {
		var value int
		return f.observer.QueryRow(ctx, "SELECT value FROM "+f.table+" WHERE id=1").Scan(&value) == nil && value == 37
	}}
	assertSQLLostCommittedReply(t, f.sqlExecutionFixture, transport, f.value)
}

func testSQLServerSQLLostReply(t *testing.T) {
	f := newSQLServerExecutionFixture(t)
	transport := &sqlLostReplyTransport{lifetime: t.Context(), witness: func(ctx context.Context) bool {
		var value int
		return f.observer.QueryRowContext(ctx, "SELECT value FROM "+f.table+" WHERE id=1").Scan(&value) == nil && value == 37
	}}
	assertSQLLostCommittedReply(t, f.sqlExecutionFixture, transport, f.value)
	if transport.dials.Load() != 1 {
		t.Fatalf("SQL Server retried its connection: %d", transport.dials.Load())
	}
}
