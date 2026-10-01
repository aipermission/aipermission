package conformance_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolecatalog"
	"github.com/jackc/pgx/v5"
)

func TestPostgresRoleCleanupDrainsPriorTablespaceOwnershipBeforeScopeCheck(t *testing.T) {
	requireConformance(t)
	for _, outcome := range []string{"commit", "rollback", "timeout", "cancel"} {
		t.Run(outcome, func(t *testing.T) {
			fixture := newPostgresRoleFenceFixture(t)
			space := createPostgresSharedOwnershipFixture(t, fixture)
			initial := beginPostgresRoleFence(t, fixture.primary)
			record := fixture.record(t, initial)
			if err := initial.Rollback(t.Context()); err != nil {
				t.Fatal(err)
			}
			writer, err := fixture.other.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			cleanupPostgresRoleFenceTransaction(t, writer)
			if _, err := writer.Exec(t.Context(), "ALTER TABLESPACE "+pgx.Identifier{space}.Sanitize()+" OWNER TO "+pgx.Identifier{fixture.role}.Sanitize()); err != nil {
				t.Fatal(err)
			}
			cleanup := beginPostgresRoleFence(t, fixture.primary)
			type result struct {
				plan []string
				err  error
			}
			finished := make(chan result, 1)
			planCtx, cancelPlan := context.WithCancel(t.Context())
			done := make(chan struct{})
			t.Cleanup(func() {
				cancelPlan()
				select {
				case <-done:
				case <-time.After(8 * time.Second):
					t.Error("shared ownership planner did not stop after cancellation")
				}
			})
			go func() {
				defer close(done)
				plan, err := rolecatalog.CleanupPlan(planCtx, cleanup, record)
				finished <- result{plan, err}
			}()
			waitPostgresSharedOwnershipBlocker(t, writer, fixture.other.PgConn().PID(), fixture.primary.PgConn().PID())
			if outcome == "cancel" {
				cancelPlan()
			} else if outcome == "commit" {
				if err := writer.Commit(t.Context()); err != nil {
					t.Fatal(err)
				}
			} else if outcome == "rollback" {
				if err := writer.Rollback(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			var got result
			waitLimit := 8 * time.Second
			if outcome == "cancel" {
				waitLimit = 2 * time.Second
			}
			select {
			case got = <-finished:
			case <-time.After(waitLimit):
				t.Fatal("shared ownership drain exceeded its deadline")
			}
			if outcome == "cancel" && (!errors.Is(got.err, context.Canceled) || !fixture.primary.IsClosed()) {
				t.Fatalf("cancellation did not stop the planner and close its blocked connection: %v, closed=%v", got.err, fixture.primary.IsClosed())
			}
			if outcome == "rollback" {
				if got.err != nil || len(got.plan) == 0 {
					t.Fatalf("acknowledged prior rollback did not release planning: %#v %v", got.plan, got.err)
				}
			} else if got.err == nil || got.plan != nil {
				t.Fatalf("prior ownership %s escaped cleanup boundary: %#v %v", outcome, got.plan, got.err)
			}
			if outcome == "commit" && !strings.Contains(got.err.Error(), "shared ownership") {
				t.Fatalf("fresh scope check missed committed tablespace: %v", got.err)
			}
			_ = cleanup.Rollback(t.Context())
			_ = writer.Rollback(t.Context())
			// Verify with the independent connection: timeout may close the cleanup
			// connection, but no lifecycle SQL was authorized or executed.
			var owner string
			if err := fixture.other.QueryRow(t.Context(), `SELECT owner.rolname FROM pg_catalog.pg_tablespace AS space
 JOIN pg_catalog.pg_roles AS owner ON owner.oid = space.spcowner WHERE space.spcname = $1`, space).Scan(&owner); err != nil {
				t.Fatal(err)
			}
			want := "aipermission"
			if outcome == "commit" {
				want = fixture.role
			}
			if owner != want {
				t.Fatalf("out-of-scope tablespace was reassigned: %q, want %q", owner, want)
			}
			if outcome == "cancel" {
				fixture.cleanup(t)
			}
		})
	}
}
