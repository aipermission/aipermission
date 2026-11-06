package conformance_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresReadonlyLexicalBoundariesRealService(t *testing.T) {
	requireConformance(t)
	conn := connectPostgresPolicyFixture(t)
	defer conn.Close(context.Background())
	_, err := conn.Exec(t.Context(), `CREATE FUNCTION public.aipermission_lexical_probe() RETURNS integer LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(77077); RETURN 77; END $$`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `DROP FUNCTION public.aipermission_lexical_probe()`) }()
	connector := postgresconnector.New()
	assertPostgresHiddenStatementControl(t, conn)
	runtime := postgresConformanceRuntime(t)
	var version int
	if err := conn.QueryRow(t.Context(), "SELECT current_setting('server_version_num')::integer").Scan(&version); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"SELECT 1, public.aipermission_lexical_probe\v()",
		"SELECT 1--\r, public.aipermission_lexical_probe()",
	} {
		t.Run(query, func(t *testing.T) {
			assertReadonlyLexicalServerControl(t, conn, query, version)
			request := connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile,
				ActionName: postgresconnector.ActionQueryReadonly, Input: map[string]any{"sql": query, "max_rows": 5}}
			if _, err := connector.PrepareAction(t.Context(), request); err == nil {
				t.Fatal("lexically hidden function accepted")
			}
			request.Input["sql"] = "SELECT 1"
			prepared, err := connector.PrepareAction(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			prepared.Payload["sql"] = query
			if _, err := connector.ExecuteAction(t.Context(), runtime, prepared); err == nil {
				t.Fatal("persisted hidden function accepted")
			}
		})
	}
	result := executeAction(t, connector, runtime, postgresconnector.ActionQueryReadonly,
		map[string]any{"sql": "SELECT lower('LEXICAL-CONTROL') AS marker", "max_rows": 5})
	assertResultContains(t, result, "lexical-control")
}

func assertPostgresHiddenStatementControl(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(t.Context(), "CREATE TEMP TABLE lexical_statement_canary (value integer)"); err != nil {
		t.Fatal(err)
	}
	rows, err := conn.Query(t.Context(), "SELECT 1--\r; INSERT INTO lexical_statement_canary VALUES (1)")
	if rows != nil {
		rows.Close()
		err = rows.Err()
	}
	var serverError *pgconn.PgError
	if !errors.As(err, &serverError) || serverError.Code != "42601" {
		t.Fatalf("extended query did not reject hidden second statement: %v", err)
	}
	var count int
	if err := conn.QueryRow(t.Context(), "SELECT count(*) FROM lexical_statement_canary").Scan(&count); err != nil || count != 0 {
		t.Fatalf("hidden second statement changed canary: count=%d err=%v", count, err)
	}
}

func assertReadonlyLexicalServerControl(t *testing.T, conn *pgx.Conn, query string, version int) {
	t.Helper()
	tx, err := conn.BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var first, second int
	err = tx.QueryRow(t.Context(), query).Scan(&first, &second)
	if version < 170000 && strings.ContainsRune(query, '\v') {
		var serverError *pgconn.PgError
		if !errors.As(err, &serverError) || serverError.Code != "42601" {
			t.Fatalf("pre-17 VT control should reject syntax: %v", err)
		}
		t.Log("server before17 rejects VT; connector policy still must reject the hidden call")
		return
	}
	if err != nil || first != 1 || second != 77 {
		t.Fatalf("server lexical control: first=%d second=%d err=%v", first, second, err)
	}
	var held bool
	if err := tx.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' AND objid=77077)`).Scan(&held); err != nil || !held {
		t.Fatalf("read-only transaction did not execute the lock side effect: held=%v err=%v", held, err)
	}
}
