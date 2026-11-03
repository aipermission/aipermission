package sqlserverconnector

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	mssql "github.com/microsoft/go-mssqldb"
)

func fixtureRuntime() connectors.RuntimeContext {
	return connectors.RuntimeContext{
		Target:  connectors.TargetView{ID: 1, ProjectID: 4, Ref: "sqlserver:1:2", ConnectorKind: Kind, Config: map[string]any{"host": "db.example.test", "database": "app"}},
		Profile: connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Kind: "username_password", Public: map[string]any{"username": "reader"}},
		Secrets: testSecrets{}, Capabilities: &testTransport{},
	}
}

type testSecrets struct{ err error }

func (s testSecrets) GetSecret(context.Context, string) (string, error) { return "fixture-only", s.err }

type testTransport struct {
	calls   int
	request connectors.NetworkDialRequest
	err     error
}

func (*testTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}
func (s *testTransport) RuntimeCapability(string) connectors.RuntimeCapability { return s }
func (s *testTransport) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	s.calls++
	s.request = request
	if s.err != nil {
		return nil, s.err
	}
	return nil, ctx.Err()
}

func TestConnectionPreservesBoundaryPortsWithoutNetwork(t *testing.T) {
	for _, port := range []int{1, 65535} {
		t.Run(fmt.Sprint(port), func(t *testing.T) {
			runtime := fixtureRuntime()
			runtime.Target.Config["port"] = port
			transport := &testTransport{err: errors.New("offline boundary fixture")}
			runtime.Capabilities = transport
			db, err := connection(t.Context(), runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.PingContext(t.Context()); err == nil || transport.calls != 1 || transport.request.Port != port {
				t.Fatalf("connection did not preserve port %d: calls=%d, request port=%d, err=%v", port, transport.calls, transport.request.Port, err)
			}
		})
	}
}

func TestTargetValidationAndTLS(t *testing.T) {
	for _, schema := range New().CredentialSchemas() {
		if err := connectors.ValidateCredentialSchemaDefinition(schema); err != nil {
			t.Fatal(err)
		}
	}
	config := fixtureRuntime().Target.Config
	if err := New().ValidateTargetConfig(config); err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := targetTLS(config)
	if err != nil || tlsConfig == nil || tlsConfig.InsecureSkipVerify || tlsConfig.ServerName != "db.example.test" || tlsConfig.MinVersion < 0x0303 {
		t.Fatalf("unsafe TLS default: %#v / %v", tlsConfig, err)
	}
	for _, entry := range []struct {
		key   string
		value any
	}{
		{"host", "db\\SQLEXPRESS"}, {"host", "tcp:db"}, {"host", "db;password=other"}, {"port", 0}, {"port", 65536}, {"port", 1.5},
		{"port", int64(math.MinInt64)}, {"port", int64(math.MaxInt32) + 1}, {"port", int64(math.MaxInt64)},
		{"database", ""}, {"database", strings.Repeat("a", 129)}, {"tls_mode", "trust"}, {"connection_mode", "over_ssh"},
		{"ca_certificate", "not a PEM"}, {"ca_certificate", "PRIVATE KEY"}, {"tls_server_name", "host/other"},
	} {
		t.Run(fmt.Sprintf("%s=%v", entry.key, entry.value), func(t *testing.T) {
			copy := map[string]any{}
			for key, value := range config {
				copy[key] = value
			}
			copy[entry.key] = entry.value
			if err := New().ValidateTargetConfig(copy); err == nil {
				t.Fatalf("accepted %#v", copy)
			}
		})
	}
	config["tls_mode"] = "disable"
	if result, err := targetTLS(config); result != nil || err != nil {
		t.Fatalf("explicit disabled TLS: %#v %v", result, err)
	}
	config["tls_server_name"] = "db.test"
	if _, err := targetTLS(config); err == nil {
		t.Fatal("accepted contradictory TLS settings")
	}
}

func TestCredentialIdentityAndSecretRetention(t *testing.T) {
	profile := fixtureRuntime().Profile
	if err := New().ValidateCredentialProfile(profile.Kind, profile.Public, nil, &profile); err != nil {
		t.Fatal(err)
	}
	if err := New().ValidateCredentialProfile(profile.Kind, profile.Public, nil, nil); err == nil {
		t.Fatal("created without password")
	}
	if err := New().ValidateCredentialProfile(profile.Kind, map[string]any{"username": "other"}, nil, &profile); err == nil {
		t.Fatal("changed identity without password")
	}
	if err := New().ValidateCredentialProfile(profile.Kind, profile.Public, map[string]any{"password": ""}, &profile); err == nil {
		t.Fatal("accepted blank replacement")
	}
	if err := New().ValidateCredentialProfile(profile.Kind, profile.Public, map[string]any{"password": "new-value"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionFailsBeforeSecretsOnWrongIdentity(t *testing.T) {
	runtime := fixtureRuntime()
	runtime.Target.Ref = "sqlserver:1:99"
	runtime.Secrets = testSecrets{err: errors.New("must not resolve")}
	if _, err := connection(t.Context(), runtime); !errors.Is(err, errConfig) {
		t.Fatal(err)
	}
	runtime = fixtureRuntime()
	runtime.Secrets = testSecrets{err: errors.New("lease expired")}
	if _, err := connection(t.Context(), runtime); !errors.Is(err, connectors.ErrSecretProvider) {
		t.Fatal(err)
	}
}

func TestTransportPreservesEndpointAndRejectsRedirect(t *testing.T) {
	transport := &testTransport{}
	dialer := transportDialer{transport: transport, request: connectors.NetworkDialRequest{SourceTargetRef: "sqlserver:1:2", SourceProjectID: 4, Mode: "direct", Host: "db.test", Port: 1433}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := dialer.DialContext(ctx, "tcp", "db.test:1433"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if transport.calls != 1 || transport.request.SourceProjectID != 4 || dialer.HostName() != "db.test" {
		t.Fatal("transport lost identity")
	}
	for _, address := range []string{"other.test:1433", "db.test:1444"} {
		if _, err := dialer.DialContext(t.Context(), "tcp", address); err == nil {
			t.Fatal("accepted redirect")
		}
	}
	if transport.calls != 1 {
		t.Fatal("redirect reached transport")
	}
}

func TestConnectionErrorClassification(t *testing.T) {
	for _, entry := range []struct {
		err  error
		want connectors.TestStatus
	}{
		{errConfig, connectors.TestFailedConfig}, {errCredential, connectors.TestFailedConfig}, {errTransport, connectors.TestFailedConfig},
		{connectors.ErrSecretProvider, connectors.TestUnknownError}, {mssql.Error{Number: 18456}, connectors.TestFailedAuth},
		{mssql.Error{Number: 229}, connectors.TestFailedPermission}, {mssql.Error{Number: 4060}, connectors.TestFailedPermission},
		{&tls.CertificateVerificationError{Err: errors.New("untrusted")}, connectors.TestFailedTLS},
		{context.DeadlineExceeded, connectors.TestFailedNetwork}, {errors.New("fixture failure"), connectors.TestUnknownError},
		{&net.DNSError{Err: "no such host", Name: "db-tls.example.test"}, connectors.TestFailedNetwork},
		{fmt.Errorf("connect: %w", errors.New("TLS Handshake failed: untrusted certificate")), connectors.TestFailedTLS},
		{errors.New("server does not support encryption"), connectors.TestFailedTLS},
	} {
		if got := classifyTestError(entry.err); got != entry.want {
			t.Fatalf("%T classified %s, want %s", entry.err, got, entry.want)
		}
	}
	result, err := New().TestConnection(t.Context(), connectors.RuntimeContext{})
	if err != nil || result.Status != connectors.TestFailedConfig {
		t.Fatalf("%#v / %v", result, err)
	}
}
