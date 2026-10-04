package connectortransport

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/projects"
)

type tcpAdapterFunc func(context.Context, connectorapi.PeerIdentityGateway, connectorapi.LiveConsoleRuntime, string, string, string) (net.Conn, error)

func (adapter tcpAdapterFunc) DialConnectorTCP(ctx context.Context, peer connectorapi.PeerIdentityGateway, runtime connectorapi.LiveConsoleRuntime, ref, network, address string) (net.Conn, error) {
	return adapter(ctx, peer, runtime, ref, network, address)
}

func TestNativeNetworkProjectIdentityPrecedesAdapterLookup(t *testing.T) {
	for _, name := range []string{"source", "project", "source-over-project", "foreign-source", "foreign-project", "missing-source", "no-source", "archived-project"} {
		t.Run(name, func(t *testing.T) {
			fixture := newNativeTransportFixture(t)
			other, err := projects.NewStore(fixture.database).Create(t.Context(), "Other fixture")
			if err != nil {
				t.Fatal(err)
			}
			foreignSource := fixture.addTarget(t, other.ID, "service", "Other source")
			request := connectors.NetworkDialRequest{
				Mode: "connector", Host: "127.0.0.1", Port: 4321,
				SourceTargetRef: fixture.source, TransportTargetRef: fixture.carrier,
			}
			allow := name == "source" || name == "project" || name == "source-over-project"
			switch name {
			case "project":
				request.SourceTargetRef, request.SourceProjectID = "", fixture.project
			case "source-over-project":
				request.SourceProjectID = other.ID
			case "foreign-source":
				request.SourceTargetRef, request.SourceProjectID = foreignSource, fixture.project
			case "foreign-project":
				request.SourceTargetRef, request.SourceProjectID = "", other.ID
			case "missing-source":
				request.SourceTargetRef, request.SourceProjectID = "service:999999:999999", fixture.project
			case "no-source":
				request.SourceTargetRef = ""
			case "archived-project":
				// Normal archival rejects active targets; inject stale project state.
				if _, err := fixture.database.ExecContext(t.Context(), `UPDATE projects SET status = 'archived' WHERE id = ?`, fixture.project); err != nil {
					t.Fatal(err)
				}
			}
			calls, lookups := 0, 0
			wantErr := errors.New("uncertain dial result")
			started := time.Now()
			adapter := tcpAdapterFunc(func(ctx context.Context, peer connectorapi.PeerIdentityGateway, runtime connectorapi.LiveConsoleRuntime, ref, network, address string) (net.Conn, error) {
				calls++
				fixture.requireDeliveryHeld(t)
				if ref != fixture.carrier || network != "tcp" || address != "127.0.0.1:4321" || peer.ConnectorTrustStorePath() != "fixture-trust" {
					t.Errorf("adapter routing = %q %q %q trust=%q", ref, network, address, peer.ConnectorTrustStorePath())
				}
				deadline, ok := ctx.Deadline()
				if !ok || deadline.Before(started.Add(12*time.Second)) || deadline.After(time.Now().Add(12*time.Second)) {
					t.Errorf("network deadline = %v", deadline)
				}
				if _, _, err := runtime.ResolveConnectorActionTarget(ctx, fixture.carrier); err != nil {
					t.Errorf("carrier scope unavailable: %v", err)
				}
				if _, _, err := runtime.ResolveConnectorActionTarget(ctx, fixture.source); err == nil {
					t.Error("adapter scope admitted foreign connector kind")
				}
				return nil, wantErr
			})
			transport := Network{Approved: fixture.approved(t, connectors.NetworkTransportCapabilityName), Dependencies: Dependencies{
				Runtime: fixture.runtime, TrustStorePath: func() string { return "fixture-trust" },
				AdapterFor: func(kind string) connectorapi.Adapter {
					lookups++
					if kind != "carrier" {
						t.Errorf("provider kind = %q", kind)
					}
					return adapter
				},
			}}
			conn, err := transport.DialConnectorTCP(t.Context(), request)
			if allow {
				if conn != nil || err != wantErr || calls != 1 || lookups != 1 {
					t.Fatalf("single dial: conn=%t err=%v calls=%d lookups=%d", conn != nil, err, calls, lookups)
				}
			} else if conn != nil || err == nil || calls != 0 || lookups != 0 {
				t.Fatalf("invalid identity reached adapter: conn=%t err=%v calls=%d lookups=%d", conn != nil, err, calls, lookups)
			}
			fixture.requireQuiescent(t)
		})
	}
}

func TestDirectNetworkUsesDisposableLoopbackWithoutAdapter(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var serverErr error
	go func() {
		defer close(done)
		server, err := listener.Accept()
		if err != nil {
			serverErr = err
			return
		}
		defer server.Close()
		if err := server.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			serverErr = err
			return
		}
		data := make([]byte, 4)
		if _, err := io.ReadFull(server, data); err != nil {
			serverErr = err
			return
		}
		if string(data) != "ping" {
			serverErr = errors.New("unexpected loopback bytes")
			return
		}
		_, serverErr = io.WriteString(server, "pong")
	}()
	t.Cleanup(func() {
		listener.Close()
		<-done
	})
	calls := 0
	transport := Network{Dependencies: Dependencies{AdapterFor: func(string) connectorapi.Adapter {
		calls++
		return nil
	}}}
	conn, err := transport.DialConnectorTCP(nil, connectors.NetworkDialRequest{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(conn, "ping"); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4)
	if _, err := io.ReadFull(conn, data); err != nil {
		t.Fatal(err)
	}
	<-done
	if string(data) != "pong" || serverErr != nil || calls != 0 {
		t.Fatalf("loopback exchange = %q server=%v adapter calls=%d", data, serverErr, calls)
	}
}
