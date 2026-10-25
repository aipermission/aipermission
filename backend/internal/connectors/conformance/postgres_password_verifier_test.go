package conformance_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func assertManagedRolePasswordAuthentication(t *testing.T, admin *pgx.Conn, role, password string) {
	t.Helper()
	var verifier string
	if err := admin.QueryRow(t.Context(), "SELECT rolpassword FROM pg_authid WHERE rolname = $1", role).Scan(&verifier); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(verifier, "SCRAM-SHA-256$") || strings.Contains(verifier, password) {
		t.Fatal("managed role did not store an opaque SCRAM verifier")
	}
	config := admin.Config().Copy()
	config.User, config.Password = role, password
	connection, err := pgx.ConnectConfig(t.Context(), config)
	if err != nil {
		t.Fatalf("generated managed password cannot authenticate: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	closePostgresRoleFixtureConnection(t, ctx, connection)
	config.Password = "intentionally-incorrect-fixture-password"
	wrong, err := pgx.ConnectConfig(t.Context(), config)
	if err == nil {
		closePostgresRoleFixtureConnection(t, ctx, wrong)
		t.Fatal("managed password check did not exercise password authentication")
	}
}
