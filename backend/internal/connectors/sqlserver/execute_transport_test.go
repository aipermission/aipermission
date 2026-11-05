package sqlserverconnector

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type executionWireTransport struct {
	net.Conn
	calls int
}

func (*executionWireTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (p *executionWireTransport) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.NetworkTransportCapabilityName {
		return p
	}
	return nil
}

func (p *executionWireTransport) DialConnectorTCP(context.Context, connectors.NetworkDialRequest) (net.Conn, error) {
	p.calls++
	return p.Conn, nil
}

type executionWireTranscript struct {
	queries []string
	err     error
}

type executionWireReadGate struct {
	net.Conn
	armed   <-chan struct{}
	reading chan struct{}
	once    sync.Once
}

func (c *executionWireReadGate) Read(p []byte) (int, error) {
	select {
	case <-c.armed:
		c.once.Do(func() { close(c.reading) })
	default:
	}
	return c.Conn.Read(p)
}

func executionWireRuntime(t *testing.T, eofBeforeMessage bool) (connectors.RuntimeContext, *executionWireTransport, <-chan executionWireTranscript) {
	t.Helper()
	client, peer := net.Pipe()
	armed, reading := make(chan struct{}), make(chan struct{})
	transport := &executionWireTransport{Conn: &executionWireReadGate{Conn: client, armed: armed, reading: reading}}
	done := make(chan executionWireTranscript, 1)
	go func() {
		defer close(done)
		defer peer.Close()
		_ = peer.SetDeadline(time.Now().Add(10 * time.Second))
		queries, err := serveExecutionWire(t.Context(), peer, eofBeforeMessage, armed, reading)
		done <- executionWireTranscript{queries: queries, err: err}
	}()
	t.Cleanup(func() {
		_ = client.Close()
		_ = peer.Close()
		select {
		case transcript, ok := <-done:
			if ok && transcript.err != nil {
				t.Errorf("execution wire fixture: %v (queries=%q)", transcript.err, transcript.queries)
			}
		case <-time.After(time.Second):
			t.Error("execution wire fixture did not stop")
		}
	})
	runtime := fixtureRuntime()
	runtime.Target.Config["tls_mode"] = "disable"
	runtime.Capabilities = transport
	return runtime, transport, done
}

// Minimal in-memory TDS peer: real pinned-driver login, setup, SQL batches and
// transaction-state result, without a live SQL Server or listener.
func serveExecutionWire(ctx context.Context, conn net.Conn, eofBeforeMessage bool, armed chan<- struct{}, reading <-chan struct{}) (queries []string, err error) {
	if _, err := readExecutionPacket(conn, 0x12); err != nil {
		return nil, err
	}
	// PRELOGIN ENCRYPTION = NOT_SUP, then a TDS 7.4 LOGINACK and final DONE.
	if err := writeExecutionReply(conn, []byte{1, 0, 6, 0, 1, 0xff, 2}); err != nil {
		return nil, err
	}
	if _, err := readExecutionPacket(conn, 0x10); err != nil {
		return nil, err
	}
	ack := []byte{0xad, 10, 0, 1, 0x74, 0, 0, 4, 0, 0, 0, 0, 0}
	if err := writeExecutionReply(conn, append(ack, executionWireDone(0)...)); err != nil {
		return nil, err
	}
	for {
		payload, err := readExecutionPacket(conn, 1)
		if err != nil {
			return queries, err
		}
		if len(payload) < 4 {
			return queries, fmt.Errorf("missing SQL batch headers")
		}
		headerSize := int(binary.LittleEndian.Uint32(payload))
		if headerSize < 4 || headerSize > len(payload) || (len(payload)-headerSize)%2 != 0 {
			return queries, fmt.Errorf("invalid SQL batch headers")
		}
		units := make([]uint16, (len(payload)-headerSize)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(payload[headerSize+2*i:])
		}
		query := string(utf16.Decode(units))
		queries = append(queries, query)
		if len(queries) == 1 {
			if !strings.HasPrefix(query, "SET QUOTED_IDENTIFIER ON; SET TEXTSIZE 131072; SET LOCK_TIMEOUT ") {
				return queries, fmt.Errorf("expected native session setup, got %q", query)
			}
			if err := writeExecutionReply(conn, executionWireDone(0)); err != nil {
				return queries, err
			}
			close(armed)
			continue
		}
		if eofBeforeMessage {
			// Close only once the driver has passed SetDeadline and entered Read,
			// so this exercises EOF rather than net.Pipe's closed deadline error.
			select {
			case <-reading:
			case <-ctx.Done():
				return queries, context.Cause(ctx)
			}
			return queries, nil
		}
		var reply []byte
		switch strings.TrimSpace(query) {
		case "SELECT 7;":
			reply = executionWireIntegers(7)
		case "SELECT 8;":
			reply = executionWireIntegers(8)
		case executionTransactionSQL:
			reply = executionWireIntegers(0, 0)
		default:
			return queries, fmt.Errorf("unexpected native SQL: %q", query)
		}
		if err := writeExecutionReply(conn, reply); err != nil {
			return queries, err
		}
		if query == executionTransactionSQL {
			_, err := conn.Read(make([]byte, 1))
			if !errors.Is(err, io.EOF) {
				return queries, fmt.Errorf("expected session teardown, got %v", err)
			}
			return queries, nil
		}
	}
}

func readExecutionPacket(conn net.Conn, kind byte) ([]byte, error) {
	var payload []byte
	for {
		header := make([]byte, 8)
		if _, err := io.ReadFull(conn, header); err != nil {
			return nil, err
		}
		length := int(binary.BigEndian.Uint16(header[2:4]))
		if header[0] != kind || length < 8 {
			return nil, fmt.Errorf("unexpected TDS packet header: %x", header)
		}
		body := make([]byte, length-8)
		if _, err := io.ReadFull(conn, body); err != nil {
			return nil, err
		}
		payload = append(payload, body...)
		if header[1]&1 != 0 {
			return payload, nil
		}
	}
}

func writeExecutionReply(conn net.Conn, payload []byte) error {
	header := []byte{4, 1, 0, 0, 0, 0, 1, 0}
	binary.BigEndian.PutUint16(header[2:4], uint16(len(payload)+8))
	_, err := conn.Write(append(header, payload...))
	return err
}

func executionWireDone(rows uint64) []byte {
	done := make([]byte, 13)
	done[0] = 0xfd
	if rows != 0 {
		done[1] = 0x10
	}
	binary.LittleEndian.PutUint64(done[5:], rows)
	return done
}

func executionWireIntegers(values ...int32) []byte {
	metadata := []byte{0x81, 0, 0}
	binary.LittleEndian.PutUint16(metadata[1:], uint16(len(values)))
	for range values {
		// UserType, Flags, INT4, and a one-character UTF-16 column name.
		metadata = append(metadata, 0, 0, 0, 0, 0, 0, 0x38, 1, 'v', 0)
	}
	metadata = append(metadata, 0xd1)
	for _, value := range values {
		metadata = binary.LittleEndian.AppendUint32(metadata, uint32(value))
	}
	return append(metadata, executionWireDone(1)...)
}

func TestExecutionTransportEOFBeforeFirstMessageIsPromptlyUnknown(t *testing.T) {
	runtime, transport, done := executionWireRuntime(t, true)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	action, err := New().PrepareAction(ctx, connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile,
		ActionName: ActionExecuteSQL, Input: map[string]any{"sql": "UPDATE dbo.t SET id=1;", "timeout_seconds": 300}})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result, err := New().ExecuteAction(ctx, runtime, action)
	if err != nil || result.Status != connectors.ResultOutcomeUnknown || !strings.Contains(result.Error, "transport read: EOF") {
		t.Fatalf("transport cause lost or dispatch misclassified: %#v err=%v", result, err)
	}
	if ctx.Err() != nil || time.Since(started) > 2*time.Second {
		t.Fatalf("socket EOF waited for action deadline: elapsed=%v ctx=%v", time.Since(started), ctx.Err())
	}
	transcript := <-done
	if transcript.err != nil || transport.calls != 1 || len(transcript.queries) != 2 || result.Output.(map[string]any)["completed_batches"] != 0 {
		t.Fatalf("unexpected replay or fixture failure: calls=%d transcript=%#v result=%#v", transport.calls, transcript, result)
	}
	if !strings.Contains(transcript.queries[0], "LOCK_TIMEOUT 300000;") {
		t.Fatalf("first setup did not use selected timeout: %q", transcript.queries[0])
	}
}

func TestExecutionTransportPreservesNativeBatchesAndFinish(t *testing.T) {
	runtime, transport, done := executionWireRuntime(t, false)
	action, err := New().PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile,
		ActionName: ActionExecuteSQL, Input: map[string]any{"sql": "SELECT 7;\nGO\nSELECT 8;"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := New().ExecuteAction(t.Context(), runtime, action)
	if err != nil || result.Status != connectors.ResultCompleted {
		t.Fatalf("normal native execution failed: %#v err=%v", result, err)
	}
	transcript := <-done
	output := result.Output.(map[string]any)
	if transcript.err != nil || transport.calls != 1 || len(transcript.queries) != 4 || output["row_count"] != 2 || output["completed_batches"] != 2 || output["result_set_count"] != 2 {
		t.Fatalf("native flow changed: calls=%d transcript=%#v output=%#v", transport.calls, transcript, output)
	}
	if _, ok := runtime.Capability(connectors.NetworkTransportCapabilityName).(*executionWireTransport); !ok {
		t.Fatal("execution changed the caller's transport capability")
	}
}

func TestReadonlyTransportFailureDoesNotCancelCaller(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	runtime := fixtureRuntime()
	runtime.Capabilities = &executionWireTransport{Conn: client}
	connector, err := driverConnector(ctx, runtime, queryTimeout)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := connector.Dialer.DialContext(ctx, "tcp", "db.example.test:1433")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = peer.Close()
	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) || context.Cause(ctx) != nil {
		t.Fatalf("read transport semantics changed: err=%v cause=%v", err, context.Cause(ctx))
	}
}

type failingExecutionSocket struct {
	net.Conn
	failure error
	closed  chan struct{}
	once    sync.Once
}

func (c *failingExecutionSocket) Read([]byte) (int, error)         { return 0, c.failure }
func (c *failingExecutionSocket) Write([]byte) (int, error)        { return 0, c.failure }
func (c *failingExecutionSocket) SetDeadline(time.Time) error      { return c.failure }
func (c *failingExecutionSocket) SetReadDeadline(time.Time) error  { return c.failure }
func (c *failingExecutionSocket) SetWriteDeadline(time.Time) error { return c.failure }
func (c *failingExecutionSocket) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func TestExecutionSocketFailureCancelsFullLifetimeAndPreservesCause(t *testing.T) {
	for _, operation := range []string{"read", "write", "set deadline", "set read deadline", "set write deadline"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			client, peer := net.Pipe()
			defer peer.Close()
			failure := errors.New("owned socket failure")
			socket := &failingExecutionSocket{Conn: client, failure: failure, closed: make(chan struct{})}
			bound := connectors.BindNetworkConnectionContext(ctx, &executionNetworkConn{Conn: socket, cancel: cancel})
			defer bound.Close()
			var err error
			switch operation {
			case "read":
				_, err = bound.Read(make([]byte, 1))
			case "write":
				_, err = bound.Write([]byte{1})
			case "set deadline":
				err = bound.SetDeadline(time.Now())
			case "set read deadline":
				err = bound.SetReadDeadline(time.Now())
			case "set write deadline":
				err = bound.SetWriteDeadline(time.Now())
			}
			if !errors.Is(err, failure) || !errors.Is(context.Cause(ctx), failure) || !strings.Contains(context.Cause(ctx).Error(), "transport "+operation) {
				t.Fatalf("lost socket failure: err=%v cause=%v", err, context.Cause(ctx))
			}
			cancel(errors.New("later cleanup failure"))
			if !errors.Is(context.Cause(ctx), failure) {
				t.Fatal("cleanup replaced original transport cause")
			}
			select {
			case <-socket.closed:
			case <-time.After(time.Second):
				t.Fatal("transport failure did not close the bound socket")
			}
		})
	}
}
