package sqlserverconnector

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortransport"
)

type lifetimeCapabilities struct{}

func (lifetimeCapabilities) RuntimeCapability(string) connectors.RuntimeCapability {
	return connectortransport.Network{}
}

type lifetimePipeTransport struct{ conn net.Conn }

func (lifetimePipeTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}
func (p lifetimePipeTransport) DialConnectorTCP(context.Context, connectors.NetworkDialRequest) (net.Conn, error) {
	return p.conn, nil
}

func TestDriverDialContextIsNotTheSocketLifetime(t *testing.T) {
	operation, stopOperation := context.WithCancel(t.Context())
	defer stopOperation()
	dial, stopDial := context.WithCancel(operation)
	defer stopDial()
	client, peer := net.Pipe()
	defer peer.Close()
	driver := transportDialer{lifetime: operation, transport: lifetimePipeTransport{conn: client}, request: connectors.NetworkDialRequest{Host: "db.test", Port: 1433}}
	bound, err := driver.DialContext(dial, "tcp", "db.test:1433")
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Close()
	stopDial()
	_ = bound.SetReadDeadline(time.Now().Add(time.Second))
	written := make(chan error, 1)
	go func() { _, err := peer.Write([]byte{1}); written <- err }()
	if _, err := bound.Read(make([]byte, 1)); err != nil {
		t.Fatalf("dial cleanup closed the live operation socket: %v", err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	stopOperation()
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("operation cancellation did not close socket: %v", err)
	}
}

func silentPreloginTarget(t *testing.T, trickle bool) (connectors.RuntimeContext, <-chan net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stop, done := make(chan struct{}), make(chan struct{})
	accepted := make(chan net.Conn, 1)
	go func() {
		defer close(done)
		peer, err := listener.Accept()
		if err != nil {
			return
		}
		defer peer.Close()
		accepted <- peer
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if trickle {
					if _, err := peer.Write([]byte{4}); err != nil {
						return
					}
				}
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("prelogin fixture did not stop")
		}
	})
	runtime := fixtureRuntime()
	runtime.Target.Config["host"] = "127.0.0.1"
	runtime.Target.Config["port"] = listener.Addr().(*net.TCPAddr).Port
	runtime.Capabilities = lifetimeCapabilities{}
	return runtime, accepted
}

func TestPreloginHonorsCallerDeadlineAfterSuccessfulDial(t *testing.T) {
	runtime, accepted := silentPreloginTarget(t, false)
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	done := make(chan connectors.TestResult, 1)
	go func() { result, _ := New().TestConnection(ctx, runtime); done <- result }()
	var peer net.Conn
	select {
	case peer = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("TCP dial did not reach fixture")
	}
	defer peer.Close()
	select {
	case result := <-done:
		if result.Status != connectors.TestFailedNetwork || ctx.Err() != context.DeadlineExceeded {
			t.Fatalf("wrong timeout result: %#v", result)
		}
	case <-time.After(time.Second):
		_ = peer.Close()
		<-done
		t.Fatal("prelogin ignored caller deadline after TCP dial")
	}
}

func TestPreloginTrickleCannotExtendAbsoluteActionDeadline(t *testing.T) {
	runtime, accepted := silentPreloginTarget(t, true)
	done := make(chan connectors.TestResult, 1)
	started := time.Now()
	go func() { result, _ := New().TestConnection(t.Context(), runtime); done <- result }()
	var peer net.Conn
	select {
	case peer = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("TCP dial did not reach fixture")
	}
	defer peer.Close()
	select {
	case result := <-done:
		if result.Status != connectors.TestFailedNetwork || time.Since(started) < queryTimeout-time.Second {
			t.Fatalf("unexpected early failure: %#v", result)
		}
	case <-time.After(queryTimeout + 2*time.Second):
		_ = peer.Close()
		<-done
		t.Fatal("trickling prelogin extended absolute deadline")
	}
}
