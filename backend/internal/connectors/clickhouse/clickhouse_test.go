package clickhouseconnector

import (
	"context"
	"crypto/tls"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/connectortest"
)

func TestConnectorSchemaAndActions(t *testing.T) {
	connector := New()
	if connector.Kind() != Kind || connector.Label() != Label || connector.Version() != Version {
		t.Fatalf("unexpected connector metadata: %s %s %s", connector.Kind(), connector.Label(), connector.Version())
	}
	for _, field := range connector.TargetSchema().Fields {
		if field.Secret || field.Type == connectors.FieldSecret || field.Type == connectors.FieldMultilineSecret {
			t.Fatalf("target field %q must not be secret", field.Name)
		}
	}
	credentials := connector.CredentialSchemas()
	if len(credentials) != 1 || !hasField(credentials[0].Schema, "username") || !hasSecretField(credentials[0].Schema, "password") {
		t.Fatalf("unexpected credential schema: %#v", credentials)
	}
	actions, err := connector.GetActionList(context.Background(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatalf("get actions: %v", err)
	}
	expected := []string{ActionGetDatabases, ActionGetTables, ActionDescribeTable, ActionQueryReadonly}
	if len(actions) != len(expected) {
		t.Fatalf("actions = %d, want %d", len(actions), len(expected))
	}
	for index, action := range actions {
		if action.Name != expected[index] || action.Risk != connectors.RiskRead || action.Label == "" || action.Description == "" {
			t.Fatalf("unexpected action %d: %#v", index, action)
		}
	}
}

func TestPrepareActionsAreDeterministicAndBounded(t *testing.T) {
	connector := New()
	target := connectors.TargetView{
		ID: 4, Ref: "clickhouse:4:8", ConnectorKind: Kind, Name: "analytics",
		Config: map[string]any{"connection_mode": "over_ssh", "transport_target_ref": "ssh:2:3", "host": "127.0.0.1", "port": 9000, "database": "default", "tls_mode": "disable"},
	}
	profile := connectors.CredentialProfileView{ID: 8, TargetID: 4, ConnectorKind: Kind, Kind: "username_password", Label: "readonly", Public: map[string]any{"username": "reader"}}
	tests := map[string]map[string]any{
		ActionGetDatabases:  {},
		ActionGetTables:     {"database": "analytics"},
		ActionDescribeTable: {"database": "analytics", "table": "events"},
		ActionQueryReadonly: {"sql": "SELECT * FROM events LIMIT 10", "max_rows": 5000},
	}
	for action, input := range tests {
		req := connectors.ActionRequest{Target: target, Profile: profile, ActionName: action, Input: input, Reason: "connector test"}
		connectortest.AssertPrepareActionDeterministic(t, connector, req)
		prepared, err := connector.PrepareAction(context.Background(), req)
		if err != nil {
			t.Fatalf("prepare %s: %v", action, err)
		}
		if prepared.ConnectorKind != Kind || prepared.Risk != connectors.RiskRead {
			t.Fatalf("unexpected prepared action: %#v", prepared)
		}
		if prepared.ContextMaterial["transport_target_ref"] != "ssh:2:3" {
			t.Fatalf("transport context missing: %#v", prepared.ContextMaterial)
		}
		if action == ActionQueryReadonly && prepared.Payload["max_rows"] != maxRows {
			t.Fatalf("max rows = %#v, want %d", prepared.Payload["max_rows"], maxRows)
		}
	}
}

func TestPrepareActionRejectsUnsafeQueries(t *testing.T) {
	connector := New()
	for _, sqlText := range []string{
		"INSERT INTO events VALUES (1)",
		"SELECT 1; SELECT 2",
		"SELECT * INTO OUTFILE '/tmp/events' FROM events",
	} {
		_, err := connector.PrepareAction(context.Background(), connectors.ActionRequest{
			Target:  connectors.TargetView{Ref: "clickhouse:1:2", ConnectorKind: Kind},
			Profile: connectors.CredentialProfileView{ID: 2}, ActionName: ActionQueryReadonly,
			Input: map[string]any{"sql": sqlText},
		})
		if err == nil {
			t.Fatalf("expected %q to be rejected", sqlText)
		}
	}
}

func TestPrepareActionAllowsSafeClickHouseQueries(t *testing.T) {
	for _, sqlText := range []string{
		"SELECT formatDateTime(now(), '%F')",
		"SELECT replace(name, 'old', 'new') FROM events",
		"SELECT database, table, name FROM system.columns LIMIT 10",
	} {
		_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
			Target:  connectors.TargetView{Ref: "clickhouse:1:2", ConnectorKind: Kind},
			Profile: connectors.CredentialProfileView{ID: 2}, ActionName: ActionQueryReadonly,
			Input: map[string]any{"sql": sqlText},
		})
		if err != nil {
			t.Fatalf("prepare safe ClickHouse query %q: %v", sqlText, err)
		}
	}
}

func TestTestConnectionUsesSharedNetworkTransport(t *testing.T) {
	for _, test := range []struct {
		name         string
		mode         string
		transportRef string
	}{
		{name: "direct", mode: "direct"},
		{name: "over ssh", mode: "over_ssh", transportRef: "ssh:2:3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			transportErr := errors.New("transport unavailable for test")
			transport := &recordingTransport{err: transportErr}
			runtime := connectors.RuntimeContext{
				Target: connectors.TargetView{ConnectorKind: Kind, Config: map[string]any{
					"connection_mode": test.mode, "transport_target_ref": test.transportRef, "host": "127.0.0.1", "port": 9000, "database": "default", "tls_mode": "disable",
				}},
				Profile:      connectors.CredentialProfileView{Public: map[string]any{"username": "reader"}},
				Secrets:      staticSecrets{"password": "secret"},
				Capabilities: capabilityResolver{transport: transport},
			}
			result, err := New().TestConnection(context.Background(), runtime)
			if err != nil {
				t.Fatalf("test connection: %v", err)
			}
			if result.Status != connectors.TestUnknownError || !strings.Contains(result.Message, transportErr.Error()) {
				t.Fatalf("unexpected result: %#v", result)
			}
			if transport.request.Mode != test.mode || transport.request.TransportTargetRef != test.transportRef || transport.request.Host != "127.0.0.1" || transport.request.Port != 9000 {
				t.Fatalf("unexpected dial request: %#v", transport.request)
			}
		})
	}
}

func TestConnectDistinguishesMissingOptionalPasswordFromProviderFailure(t *testing.T) {
	runtime := connectors.RuntimeContext{
		Target: connectors.TargetView{ConnectorKind: Kind, Config: map[string]any{
			"connection_mode": "direct", "host": "127.0.0.1", "port": 9000, "database": "default", "tls_mode": "disable",
		}},
		Profile:      connectors.CredentialProfileView{Public: map[string]any{"username": "reader"}},
		Capabilities: capabilityResolver{transport: &recordingTransport{}},
	}

	runtime.Secrets = staticSecrets{}
	db, err := connect(context.Background(), runtime)
	if err != nil {
		t.Fatalf("connect without optional password: %v", err)
	}
	_ = db.Close()

	providerErr := errors.New("vault lease expired")
	runtime.Secrets = failingSecrets{err: providerErr}
	_, err = connect(context.Background(), runtime)
	if !errors.Is(err, providerErr) || !strings.Contains(err.Error(), "resolve clickhouse password") {
		t.Fatalf("connect error = %v, want wrapped provider failure", err)
	}
}

func TestClickHouseTLSConfig(t *testing.T) {
	if config := clickHouseTLSConfig(connectors.TargetView{Config: map[string]any{"tls_mode": "disable", "host": "db.internal"}}); config != nil {
		t.Fatalf("disabled TLS config = %#v, want nil", config)
	}
	config := clickHouseTLSConfig(connectors.TargetView{Config: map[string]any{"tls_mode": "verify_full", "host": "db.internal"}})
	if config == nil || config.ServerName != "db.internal" || config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("unexpected verified TLS config: %#v", config)
	}
}

func TestNormalizeValue(t *testing.T) {
	if value := normalizeValue([]byte("hello")); value != "hello" {
		t.Fatalf("normalized bytes = %#v", value)
	}
}

func TestQueryRowsPreservesDuplicateColumns(t *testing.T) {
	const driverName = "aipermission-clickhouse-query-test"
	queryTestDriverOnce.Do(func() { sql.Register(driverName, queryTestDriver{}) })
	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer db.Close()

	output, err := queryRows(context.Background(), db, "SELECT id, id FROM events", 10)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	if len(output.Columns) != 2 || output.Columns[0] != "id" || output.Columns[1] != "id_2" {
		t.Fatalf("columns = %#v", output.Columns)
	}
	if output.RowCount != 1 || output.Rows[0]["id"] != int64(1) || output.Rows[0]["id_2"] != int64(2) {
		t.Fatalf("rows = %#v", output.Rows)
	}
}

func hasField(schema connectors.Schema, name string) bool {
	for _, field := range schema.Fields {
		if field.Name == name {
			return true
		}
	}
	return false
}

func hasSecretField(schema connectors.Schema, name string) bool {
	for _, field := range schema.Fields {
		if field.Name == name {
			return field.Secret && field.Type == connectors.FieldSecret
		}
	}
	return false
}

type staticSecrets map[string]string

func (s staticSecrets) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := s[name]
	if !ok {
		return "", connectors.ErrSecretNotFound
	}
	return value, nil
}

type failingSecrets struct{ err error }

func (s failingSecrets) GetSecret(context.Context, string) (string, error) {
	return "", s.err
}

type recordingTransport struct {
	request connectors.NetworkDialRequest
	err     error
}

func (*recordingTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (t *recordingTransport) DialConnectorTCP(_ context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	t.request = request
	return nil, t.err
}

type capabilityResolver struct{ transport connectors.NetworkTransport }

func (r capabilityResolver) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.NetworkTransportCapabilityName {
		return r.transport
	}
	return nil
}

type queryTestDriver struct{}

var queryTestDriverOnce sync.Once

func (queryTestDriver) Open(string) (driver.Conn, error) { return queryTestConn{}, nil }

type queryTestConn struct{}

func (queryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}
func (queryTestConn) Close() error { return nil }
func (queryTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported")
}
func (queryTestConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &queryTestRows{}, nil
}

type queryTestRows struct{ returned bool }

func (*queryTestRows) Columns() []string { return []string{"id", "id"} }
func (*queryTestRows) Close() error      { return nil }
func (r *queryTestRows) Next(values []driver.Value) error {
	if r.returned {
		return io.EOF
	}
	r.returned = true
	values[0] = int64(1)
	values[1] = int64(2)
	return nil
}
