package conformance_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolecatalog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresRoleFenceDoesNotFallbackForCreateRoleOnlyAdmin(t *testing.T) {
	requireConformance(t)
	fixture := newPostgresRoleFenceFixture(t)
	role := pgx.Identifier{fixture.member}.Sanitize()
	if _, err := fixture.primary.Exec(t.Context(), `ALTER ROLE `+role+` CREATEROLE`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.primary.Exec(t.Context(), `SET ROLE `+role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = fixture.primary.Exec(ctx, `RESET ROLE`)
	})
	tx, err := rolecatalog.Begin(t.Context(), fixture.primary)
	if tx != nil {
		cleanupPostgresRoleFenceTransaction(t, tx)
		t.Fatal("missing catalog privileges exposed an unlocked transaction")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("catalog privilege failure was not preserved: %v", err)
	}
	assertPostgresRoleFenceConnectionIdle(t, fixture.primary)
}
