package conformance_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/jackc/pgx/v5"
)

func TestPostgresUnicodeTypedLiteralRealService(t *testing.T) {
	requireConformance(t)
	conn := connectPostgresPolicyFixture(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 3*time.Second)
		defer cancel()
		_ = conn.Close(ctx)
	})
	execPostgresPolicyFixture(t, conn, []string{
		`CREATE ROLE aipermission_literal_reader LOGIN PASSWORD 'literal-fixture-only'`,
		`CREATE SCHEMA "püb"`,
		`CREATE FUNCTION public.aipermission_literal_check(text) RETURNS boolean LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(65001); RETURN true; END $$`,
		`CREATE DOMAIN public."dömain" AS text CHECK (public.aipermission_literal_check(VALUE))`,
		`CREATE DOMAIN public."tÿpe" AS text CHECK (public.aipermission_literal_check(VALUE))`,
		`CREATE DOMAIN public."🗝" AS text CHECK (public.aipermission_literal_check(VALUE))`,
		`CREATE DOMAIN "püb".domain AS text CHECK (public.aipermission_literal_check(VALUE))`,
		`GRANT USAGE ON SCHEMA public, "püb" TO aipermission_literal_reader`,
		`CREATE TABLE public.aipermission_literal_fixture ("dömain" text)`,
		`INSERT INTO public.aipermission_literal_fixture VALUES ('unicode-column-control')`,
		`GRANT SELECT ON public.aipermission_literal_fixture TO aipermission_literal_reader`,
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = conn.Exec(ctx, `RESET ROLE; DROP TABLE IF EXISTS public.aipermission_literal_fixture; DROP DOMAIN IF EXISTS public."dömain", public."tÿpe", public."🗝"; DROP SCHEMA IF EXISTS "püb" CASCADE; DROP FUNCTION IF EXISTS public.aipermission_literal_check(text); DROP OWNED BY aipermission_literal_reader; DROP ROLE IF EXISTS aipermission_literal_reader`)
	})
	connector := postgresconnector.New()
	runtime := postgresConformanceRuntime(t)
	runtime.Profile.Public["username"] = "aipermission_literal_reader"
	runtime.Secrets = fixtureSecrets{"password": "literal-fixture-only"}
	assertConnection(t, connector, runtime)
	for _, query := range []string{
		`SELECT public.dömain 'x'`,
		`SELECT püb.domain 'x'`,
		`SELECT public.tÿpe 'x'`,
		`SELECT public.🗝 'x'`,
		`SELECT public.U&"d\00F6main" 'x'`,
		`SELECT public.U&"d!00F6main" UESCAPE '!' 'x'`,
		`SELECT U&"p!00FCb" UESCAPE '!'.domain E'x'`,
		`SELECT public.dömain U&'x'`,
	} {
		t.Run(query, func(t *testing.T) {
			assertReadOnlyTypedLiteralSideEffect(t, conn, query)
			_, err := connector.PrepareAction(t.Context(), connectors.ActionRequest{
				Source: "conformance", Target: runtime.Target, Profile: runtime.Profile,
				ActionName: postgresconnector.ActionQueryReadonly,
				Input:      map[string]any{"sql": query, "max_rows": 5}, Reason: "reject domain resolution",
			})
			if err == nil || !strings.Contains(err.Error(), "schema-qualified typed literals") {
				t.Fatalf("typed literal admitted during preparation: %v", err)
			}
			_, err = connector.ExecuteAction(t.Context(), runtime, connectors.PreparedAction{
				ActionName: postgresconnector.ActionQueryReadonly,
				Payload:    map[string]any{"sql": query, "max_rows": 5},
			})
			if err == nil || !strings.Contains(err.Error(), "schema-qualified typed literals") {
				t.Fatalf("typed literal admitted at execution: %v", err)
			}
		})
	}
	result := executeAction(t, connector, runtime, postgresconnector.ActionQueryReadonly, map[string]any{
		"sql": `SELECT DATE '2026-10-03' AS fixture_date`, "max_rows": 5,
	})
	assertResultContains(t, result, "2026-10-03")
	result = executeAction(t, connector, runtime, postgresconnector.ActionQueryReadonly, map[string]any{
		"sql": `SELECT U&"d\00F6main" FROM public.aipermission_literal_fixture`, "max_rows": 5,
	})
	assertResultContains(t, result, "unicode-column-control")
}

func assertReadOnlyTypedLiteralSideEffect(t *testing.T, conn *pgx.Conn, query string) {
	t.Helper()
	if _, err := conn.Exec(t.Context(), `SET ROLE aipermission_literal_reader`); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(ctx)
		_, _ = conn.Exec(ctx, `RESET ROLE`)
	}()
	var value string
	if err := tx.QueryRow(t.Context(), query).Scan(&value); err != nil || value != "x" {
		t.Fatalf("ordinary-role readonly control: value=%q err=%v", value, err)
	}
	var sideEffect bool
	if err := tx.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = pg_backend_pid() AND locktype = 'advisory')`).Scan(&sideEffect); err != nil || !sideEffect {
		t.Fatalf("readonly domain check did not acquire advisory lock: held=%v err=%v", sideEffect, err)
	}
}
