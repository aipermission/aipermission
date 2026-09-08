package conformance_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/jackc/pgx/v5"
)

func TestPostgresRealService(t *testing.T) {
	requireConformance(t)
	connector := postgresconnector.New()
	runtime := connectors.RuntimeContext{
		Target: connectors.TargetView{
			ID: 1, Ref: "postgres:1:1", ConnectorKind: postgresconnector.Kind, Name: "conformance-postgres",
			Config: map[string]any{
				"connection_mode": "direct",
				"host":            fixtureHost("AIPERMISSION_POSTGRES_HOST", "127.0.0.1"),
				"port":            fixturePort(t, "AIPERMISSION_POSTGRES_PORT", 5432),
				"database":        "aipermission",
				"ssl_mode":        "disable",
			},
		},
		Profile: connectors.CredentialProfileView{
			ID: 1, TargetID: 1, ConnectorKind: postgresconnector.Kind, Kind: "username_password", Label: "conformance",
			Public: map[string]any{"username": "aipermission"},
		},
		Secrets:      fixtureSecrets{"password": "conformance-only"},
		Capabilities: fixtureCapabilities{},
	}

	assertConnection(t, connector, runtime)
	result := executeAction(t, connector, runtime, postgresconnector.ActionQueryReadonly, map[string]any{
		"sql":      "select current_database() as database_name, 'postgres-conformance' as marker",
		"max_rows": 5,
	})
	assertResultContains(t, result, "postgres-conformance")
	assertCatalogFunctionResolutionIsolated(t, connector, runtime)
	assertImplicitCastResolutionRejected(t, connector, runtime)
}

func assertImplicitCastResolutionRejected(t *testing.T, connector connectors.Connector, runtime connectors.RuntimeContext) {
	t.Helper()
	address := net.JoinHostPort(
		fixtureHost("AIPERMISSION_POSTGRES_HOST", "127.0.0.1"),
		fmt.Sprintf("%d", fixturePort(t, "AIPERMISSION_POSTGRES_PORT", 5432)),
	)
	config, err := pgx.ParseConfig(fmt.Sprintf("postgres://aipermission:conformance-only@%s/aipermission?sslmode=disable", address))
	if err != nil {
		t.Fatalf("parse postgres implicit-cast fixture config: %v", err)
	}
	conn, err := pgx.ConnectConfig(t.Context(), config)
	if err != nil {
		t.Fatalf("connect postgres implicit-cast fixture: %v", err)
	}
	defer conn.Close(context.Background())
	statements := []string{
		`CREATE TYPE public.aipermission_custom_text AS (value text)`,
		`CREATE FUNCTION public.aipermission_custom_text_to_text(public.aipermission_custom_text) RETURNS text LANGUAGE SQL IMMUTABLE AS 'SELECT $1.value'`,
		`CREATE CAST (public.aipermission_custom_text AS text) WITH FUNCTION public.aipermission_custom_text_to_text(public.aipermission_custom_text) AS IMPLICIT`,
		`CREATE TABLE public.aipermission_cast_fixture (value public.aipermission_custom_text)`,
		`INSERT INTO public.aipermission_cast_fixture VALUES (ROW('fixture'))`,
	}
	for _, statement := range statements {
		if _, err := conn.Exec(t.Context(), statement); err != nil {
			t.Fatalf("create postgres implicit-cast fixture: %v", err)
		}
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `DROP TABLE IF EXISTS public.aipermission_cast_fixture`)
		_, _ = conn.Exec(context.Background(), `DROP CAST IF EXISTS (public.aipermission_custom_text AS text)`)
		_, _ = conn.Exec(context.Background(), `DROP FUNCTION IF EXISTS public.aipermission_custom_text_to_text(public.aipermission_custom_text)`)
		_, _ = conn.Exec(context.Background(), `DROP TYPE IF EXISTS public.aipermission_custom_text`)
	}()
	prepared, err := connector.PrepareAction(t.Context(), connectors.ActionRequest{
		Source: "conformance", Target: runtime.Target, Profile: runtime.Profile,
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select lower(value) from public.aipermission_cast_fixture", "max_rows": 5},
		Reason:     "verify custom implicit casts are rejected",
	})
	if err != nil {
		t.Fatalf("prepare postgres implicit-cast probe: %v", err)
	}
	if _, err := connector.ExecuteAction(t.Context(), runtime, prepared); err == nil || !containsErrorText(err, "custom implicit cast") {
		t.Fatalf("custom implicit cast was not rejected: %v", err)
	}
}

func assertCatalogFunctionResolutionIsolated(t *testing.T, connector connectors.Connector, runtime connectors.RuntimeContext) {
	t.Helper()
	address := net.JoinHostPort(
		fixtureHost("AIPERMISSION_POSTGRES_HOST", "127.0.0.1"),
		fmt.Sprintf("%d", fixturePort(t, "AIPERMISSION_POSTGRES_PORT", 5432)),
	)
	config, err := pgx.ParseConfig(fmt.Sprintf("postgres://aipermission:conformance-only@%s/aipermission?sslmode=disable", address))
	if err != nil {
		t.Fatalf("parse postgres conformance setup config: %v", err)
	}
	conn, err := pgx.ConnectConfig(t.Context(), config)
	if err != nil {
		t.Fatalf("connect postgres conformance setup: %v", err)
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(t.Context(), `CREATE OR REPLACE FUNCTION public.lower(integer) RETURNS integer LANGUAGE SQL AS 'SELECT 47'`); err != nil {
		t.Fatalf("create postgres overload fixture: %v", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `DROP FUNCTION IF EXISTS public.lower(integer)`)
	}()
	prepared, err := connector.PrepareAction(t.Context(), connectors.ActionRequest{
		Source: "conformance", Target: runtime.Target, Profile: runtime.Profile,
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select lower(1) as result", "max_rows": 5},
		Reason:     "verify pg_catalog-only function resolution",
	})
	if err != nil {
		t.Fatalf("prepare postgres overload probe: %v", err)
	}
	if _, err := connector.ExecuteAction(t.Context(), runtime, prepared); err == nil || !containsErrorText(err, "function lower(integer) does not exist") {
		t.Fatalf("untrusted overload was not isolated: %v", err)
	}
}

func containsErrorText(err error, text string) bool {
	return err != nil && strings.Contains(err.Error(), text)
}
