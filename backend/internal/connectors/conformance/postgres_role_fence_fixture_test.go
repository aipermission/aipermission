package conformance_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolecatalog"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/jackc/pgx/v5"
)

type postgresRoleFenceFixture struct {
	config   *pgx.ConnConfig
	primary  *pgx.Conn
	other    *pgx.Conn
	role     string
	member   string
	table    string
	database string
	marker   string
}

func newPostgresRoleFenceFixture(t *testing.T) postgresRoleFenceFixture {
	t.Helper()
	primary := connectPostgresPolicyFixture(t)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	fixture := postgresRoleFenceFixture{config: primary.Config().Copy(), primary: primary, role: "ap_fence_role_" + suffix,
		member: "ap_fence_member_" + suffix, table: "ap_fence_table_" + suffix, database: "ap_fence_db_" + suffix,
		marker: "aipermission-provision:1234567890abcdef1234567890abcdef"}
	t.Cleanup(func() { fixture.cleanup(t) })
	statements := []string{
		`CREATE ROLE ` + pgx.Identifier{fixture.role}.Sanitize(),
		`COMMENT ON ROLE ` + pgx.Identifier{fixture.role}.Sanitize() + ` IS '` + fixture.marker + `'`,
		`CREATE ROLE ` + pgx.Identifier{fixture.member}.Sanitize(),
		`CREATE TABLE ` + pgx.Identifier{"public", fixture.table}.Sanitize() + ` (id integer)`,
		`CREATE DATABASE ` + pgx.Identifier{fixture.database}.Sanitize(),
		`CREATE DATABASE ` + pgx.Identifier{fixture.database + "_idle"}.Sanitize(),
	}
	for _, statement := range statements {
		if _, err := primary.Exec(t.Context(), statement); err != nil {
			t.Fatalf("create scoped role-fence fixture: %v", err)
		}
	}
	config := primary.Config().Copy()
	config.Database = fixture.database
	var err error
	fixture.other, err = pgx.ConnectConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.other.Exec(t.Context(), `SET lock_timeout = '250ms'`); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func beginPostgresRoleFence(t *testing.T, connection *pgx.Conn) pgx.Tx {
	t.Helper()
	tx, err := rolecatalog.Begin(t.Context(), connection)
	if err != nil {
		logPostgresFixtureCatalogLocks(t, connection)
		t.Fatal(err)
	}
	cleanupPostgresRoleFenceTransaction(t, tx)
	return tx
}

func logPostgresFixtureCatalogLocks(t *testing.T, connection *pgx.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	rows, err := connection.Query(ctx, `SELECT COALESCE(activity.backend_type, 'gone'), lock.mode, lock.granted
 FROM pg_catalog.pg_locks AS lock LEFT JOIN pg_catalog.pg_stat_activity AS activity USING (pid)
 WHERE lock.locktype = 'relation' AND lock.relation = 'pg_catalog.pg_database'::regclass
 AND lock.pid <> pg_catalog.pg_backend_pid() LIMIT 12`)
	if err != nil {
		t.Logf("fixture catalog lock diagnostics unavailable: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var backend, mode string
		var granted bool
		if err := rows.Scan(&backend, &mode, &granted); err != nil {
			t.Logf("fixture lock diagnostic decode: %v", err)
			return
		}
		t.Logf("fixture pg_database lock: backend=%s mode=%s granted=%t", backend, mode, granted)
	}
	if err := rows.Err(); err != nil {
		t.Logf("fixture catalog lock diagnostics interrupted: %v", err)
	}
}

func cleanupPostgresRoleFenceTransaction(t *testing.T, tx pgx.Tx) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(ctx)
	})
}

func assertPostgresRoleFenceConnectionIdle(t *testing.T, connection *pgx.Conn) {
	t.Helper()
	if status := connection.PgConn().TxStatus(); status != 'I' {
		t.Fatalf("failed fence did not close its transaction: %q", status)
	}
	var one int
	if err := connection.QueryRow(t.Context(), `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Fatalf("connection cannot execute SQL after failed fence: %d %v", one, err)
	}
}

func (fixture postgresRoleFenceFixture) record(t *testing.T, tx pgx.Tx) rolejournal.Record {
	t.Helper()
	anchor, err := rolecatalog.CaptureAnchor(t.Context(), tx, rolejournal.Anchor{
		TargetID: 900, AdminProfileID: 901, ContextDigest: strings.Repeat("a", 64),
		TargetDigest: strings.Repeat("c", 64),
		DatabaseName: "aipermission", SuccessorName: "aipermission",
	})
	if err != nil {
		t.Fatal(err)
	}
	var oid uint32
	if err := tx.QueryRow(t.Context(), `SELECT oid FROM pg_catalog.pg_roles WHERE rolname = $1`, fixture.role).Scan(&oid); err != nil {
		t.Fatal(err)
	}
	return rolejournal.Record{Version: 1, Intent: rolejournal.Intent{Anchor: anchor, RoleName: fixture.role,
		OperationID: "1234567890abcdef1234567890abcdef"}, RoleOID: oid, Generation: "abcdef0123456789abcdef0123456789", Status: rolejournal.Provisioned}
}
