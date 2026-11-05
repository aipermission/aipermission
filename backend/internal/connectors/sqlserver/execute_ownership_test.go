package sqlserverconnector

import (
	"bytes"
	"context"
	"net"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type ownershipReadGate struct {
	net.Conn
	arm, entered, release, closed  chan struct{}
	writeOnce, readOnce, closeOnce sync.Once
	lease, original                []byte
	driverClosing                  chan struct{}
	closeCalls                     atomic.Int32
}

func (g *ownershipReadGate) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte{'S', 0, 'E', 0, 'L', 0, 'E', 0, 'C', 0, 'T', 0, ' ', 0, '7', 0}) {
		g.writeOnce.Do(func() { close(g.arm) })
	}
	return g.Conn.Write(p)
}

func (g *ownershipReadGate) Read(p []byte) (int, error) {
	n, err := g.Conn.Read(p)
	if n > 0 {
		select {
		case <-g.arm:
			g.readOnce.Do(func() {
				g.lease, g.original = p, append([]byte(nil), p[:n]...)
				close(g.entered)
				<-g.release
			})
		default:
		}
	}
	return n, err
}

func (g *ownershipReadGate) Close() error {
	err := g.Conn.Close()
	g.closeOnce.Do(func() { close(g.closed) })
	if g.closeCalls.Add(1) == 2 {
		close(g.driverClosing)
	}
	return err
}

type ownershipReadObserver struct {
	net.Conn
	first chan []byte
	once  sync.Once
}

func (o *ownershipReadObserver) Read(p []byte) (int, error) {
	n, err := o.Conn.Read(p)
	if n > 0 {
		o.once.Do(func() { o.first <- p })
	}
	return n, err
}

func TestNativeExecutionKeepsResponseBufferOwnedUntilReaderExits(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)
	previousGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previousGC)
	client, peer := net.Pipe()
	gate := &ownershipReadGate{Conn: client, arm: make(chan struct{}), entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{}), driverClosing: make(chan struct{})}
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		defer peer.Close()
		_ = peer.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = serveExecutionWire(t.Context(), peer, false, make(chan struct{}), gate.entered)
	}()
	ctx, cancel := context.WithCancel(t.Context())
	firstRuntime := fixtureRuntime()
	firstRuntime.Target.Config["tls_mode"] = "disable"
	firstRuntime.Capabilities = &executionWireTransport{Conn: gate}
	action, err := New().PrepareAction(ctx, connectors.ActionRequest{Target: firstRuntime.Target, Profile: firstRuntime.Profile, ActionName: ActionExecuteSQL, Input: map[string]any{"sql": "SELECT 7;"}})
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan struct{})
	var firstResult connectors.ActionResult
	go func() {
		defer close(firstDone)
		firstResult, _ = New().ExecuteAction(ctx, firstRuntime, action)
	}()
	t.Cleanup(func() {
		cancel()
		close(gate.release)
		_ = client.Close()
		_ = peer.Close()
		select {
		case <-firstDone:
			if firstResult.Status != connectors.ResultOutcomeUnknown {
				t.Errorf("unexpected canceled result: %#v", firstResult)
			}
		case <-time.After(2 * time.Second):
			t.Error("first execution did not join its reader")
		}
		select {
		case <-serverDone:
		case <-time.After(time.Second):
			t.Error("owned peer did not stop")
		}
	})
	select {
	case <-gate.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("native response Read was not held")
	}
	cancel()
	select {
	case <-gate.closed:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not close transport")
	}
	// The context binder closes first. The native connection's own Close must
	// also have reached transport shutdown before testing pool ownership.
	select {
	case <-gate.driverClosing:
	case <-time.After(time.Second):
		t.Fatal("native connection shutdown did not start")
	}
	select {
	case <-firstDone:
		t.Fatal("execution released resources while its response Read was still active")
	default:
	}
	secondRuntime, transport, done := executionWireRuntime(t, false)
	observer := &ownershipReadObserver{Conn: transport.Conn, first: make(chan []byte, 1)}
	transport.Conn = observer
	secondAction, err := New().PrepareAction(t.Context(), connectors.ActionRequest{Target: secondRuntime.Target, Profile: secondRuntime.Profile, ActionName: ActionExecuteSQL, Input: map[string]any{"sql": "SELECT 7;"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := New().ExecuteAction(t.Context(), secondRuntime, secondAction)
	if err != nil || result.Status != connectors.ResultCompleted {
		t.Fatalf("second execution failed: %#v %v", result, err)
	}
	<-done
	other := <-observer.first
	if &gate.lease[0] == &other[0] || !bytes.Equal(gate.lease[:len(gate.original)], gate.original) {
		t.Fatal("second native connection reused or changed the first reader's buffer")
	}
}
