package kafka

import (
	"context"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type scopedFixtureTransport struct {
	*recordingDirectTransport
	bootstrap string
	forbidden atomic.Int64
}

func (transport *scopedFixtureTransport) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	if net.JoinHostPort(request.Host, strconv.Itoa(request.Port)) != transport.bootstrap {
		transport.forbidden.Add(1)
		return nil, fmt.Errorf("test fixture refused a non-owned destination")
	}
	return transport.recordingDirectTransport.DialConnectorTCP(ctx, request)
}

func TestAdvertisedMetadataCannotEscapeScopeAcrossTransports(t *testing.T) {
	for _, mode := range []string{"direct", "over_ssh"} {
		for _, tlsEnabled := range []bool{false, true} {
			for _, destination := range []string{"169.254.169.254", "10.0.0.5"} {
				t.Run(fmt.Sprintf("%s/tls%t/%s", mode, tlsEnabled, destination), func(t *testing.T) {
					runtime, cluster, transport := hostileBrokerFixture(t, mode, tlsEnabled)
					cluster.ControlKey(int16(kmsg.Metadata), func(request kmsg.Request) (kmsg.Response, error, bool) {
						cluster.KeepControl()
						response := request.ResponseKind().(*kmsg.MetadataResponse)
						response.Brokers = []kmsg.MetadataResponseBroker{{NodeID: 0, Host: destination, Port: 80}}
						return response, nil, true
					})
					ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
					defer cancel()
					assertConfiguredDialerRejectsEndpoint(t, ctx, runtime, destination)
					result, err := New().TestConnection(ctx, runtime)
					if err != nil || result.Status == connectors.TestOK || !strings.Contains(result.Message, "scope") {
						t.Fatalf("hostile metadata accepted: %#v %v", result, err)
					}
					if transport.forbidden.Load() != 0 {
						t.Fatal("unapproved endpoint reached the transport")
					}
					for _, request := range transport.Requests() {
						if request.Mode != mode || request.SourceProjectID != 7 || request.SourceTargetRef != runtime.Target.Ref {
							t.Fatalf("transport identity changed: %#v", request)
						}
					}
				})
			}
		}
	}
}

func assertConfiguredDialerRejectsEndpoint(t *testing.T, ctx context.Context, runtime connectors.RuntimeContext, host string) {
	t.Helper()
	client, err := newClient(ctx, runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	dial, ok := client.OptValue(kgo.Dialer).(func(context.Context, string, string) (net.Conn, error))
	if !ok {
		t.Fatal("Kafka client lost its scoped transport dialer")
	}
	conn, err := dial(ctx, "tcp", net.JoinHostPort(host, "80"))
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("configured broker dial escaped scope validation: %v", err)
	}
}

func TestKafkaCertificateFailureNeverStartsSASL(t *testing.T) {
	runtime, cluster, _ := hostileBrokerFixture(t, "direct", true)
	runtime.Target.Config["tls_server_name"] = "wrong.example.invalid"
	var authenticated atomic.Int64
	cluster.ControlKey(int16(kmsg.SASLHandshake), func(kmsg.Request) (kmsg.Response, error, bool) {
		cluster.KeepControl()
		authenticated.Add(1)
		return nil, nil, false
	})
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	result, err := New().TestConnection(ctx, runtime)
	if err != nil || result.Status == connectors.TestOK || authenticated.Load() != 0 {
		t.Fatalf("certificate failure reached SASL: %#v %v exchanges=%d", result, err, authenticated.Load())
	}
	if strings.Contains(result.Message, "fixture-password") {
		t.Fatal("transport error disclosed the SASL secret")
	}
}

func hostileBrokerFixture(t *testing.T, mode string, tlsEnabled bool) (connectors.RuntimeContext, *kfake.Cluster, *scopedFixtureTransport) {
	t.Helper()
	options := []kfake.Opt{kfake.NumBrokers(1), kfake.EnableSASL(), kfake.Superuser("SCRAM-SHA-256", "reader", "fixture-password")}
	var certificate *httptest.Server
	if tlsEnabled {
		certificate = httptest.NewTLSServer(http.NotFoundHandler())
		t.Cleanup(certificate.Close)
		options = append(options, kfake.TLS(certificate.TLS.Clone()))
	}
	cluster, err := kfake.NewCluster(options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)
	transport := &scopedFixtureTransport{recordingDirectTransport: &recordingDirectTransport{}, bootstrap: cluster.ListenAddrs()[0]}
	runtime := testRuntime(cluster.ListenAddrs(), transport)
	runtime.Target.ProjectID = 7
	runtime.Target.Config["connection_mode"] = mode
	if mode == "over_ssh" {
		runtime.Target.Config["transport_target_ref"] = "ssh:1:2"
	}
	runtime.Target.Config["tls_enabled"] = tlsEnabled
	if tlsEnabled {
		runtime.Target.Config["tls_ca_pem"] = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate().Raw}))
		runtime.Target.Config["tls_server_name"] = "127.0.0.1"
	} else {
		runtime.Target.Config["allow_insecure_scram_sasl"] = true
	}
	runtime.Profile.Public = map[string]any{"mechanism": "scram_sha_256", "username": "reader"}
	runtime.Secrets = staticSecretAccessor{"password": "fixture-password"}
	return runtime, cluster, transport
}
