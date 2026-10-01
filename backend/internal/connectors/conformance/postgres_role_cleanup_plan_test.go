package conformance_test

import (
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolecatalog"
	"github.com/jackc/pgx/v5"
)

func TestPostgresRoleCleanupPlanPreservesOwnedDataAndRevokesColumnPrivileges(t *testing.T) {
	requireConformance(t)
	fixture := newPostgresRoleFenceFixture(t)
	owned := pgx.Identifier{"public", fixture.table + "_owned"}.Sanitize()
	role := pgx.Identifier{fixture.role}.Sanitize()
	for _, statement := range []string{
		"CREATE TABLE " + owned + " (id integer)",
		"INSERT INTO " + owned + " VALUES (37)",
		"ALTER TABLE " + owned + " OWNER TO " + role,
		"GRANT USAGE ON SCHEMA public TO " + role,
		"GRANT SELECT (id) ON TABLE " + pgx.Identifier{"public", fixture.table}.Sanitize() + " TO " + role,
		"GRANT SELECT ON TABLE " + pgx.Identifier{"public", fixture.table}.Sanitize() + " TO " + pgx.Identifier{fixture.member}.Sanitize(),
		"GRANT CONNECT ON DATABASE aipermission TO " + role,
	} {
		if _, err := fixture.primary.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	tx := beginPostgresRoleFence(t, fixture.primary)
	plan, err := rolecatalog.CleanupPlan(t.Context(), tx, fixture.record(t, tx))
	if err != nil {
		t.Fatal(err)
	}
	foundTableRevocation := false
	for _, statement := range plan {
		if strings.Contains(statement, "DROP OWNED") {
			t.Fatal("cleanup plan destroys owned objects")
		}
		if strings.Contains(statement, "REVOKE ALL PRIVILEGES ON table") {
			foundTableRevocation = true
		}
		if _, err := tx.Exec(t.Context(), statement); err != nil {
			t.Fatalf("execute native cleanup plan: %v", err)
		}
	}
	if !foundTableRevocation {
		t.Fatal("column-only grant was absent from revocation plan")
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	var value int
	var owner string
	if err := fixture.primary.QueryRow(t.Context(), "SELECT id FROM "+owned).Scan(&value); err != nil || value != 37 {
		t.Fatalf("owned data was lost: %d %v", value, err)
	}
	if err := fixture.primary.QueryRow(t.Context(), `SELECT owner.rolname FROM pg_catalog.pg_class AS relation
 JOIN pg_catalog.pg_roles AS owner ON owner.oid = relation.relowner
 WHERE relation.oid = $1::pg_catalog.regclass`, owned).Scan(&owner); err != nil || owner != "aipermission" {
		t.Fatalf("ownership successor mismatch: %q %v", owner, err)
	}
	var exists bool
	if err := fixture.primary.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $1)`, fixture.role).Scan(&exists); err != nil || exists {
		t.Fatalf("role not removed after acknowledged full cleanup: %v %v", exists, err)
	}
	var peerCanRead bool
	if err := fixture.primary.QueryRow(t.Context(), `SELECT pg_catalog.has_table_privilege($1, $2, 'SELECT')`,
		fixture.member, pgx.Identifier{"public", fixture.table}.Sanitize()).Scan(&peerCanRead); err != nil || !peerCanRead {
		t.Fatalf("cleanup removed another role's independent grant: %v %v", peerCanRead, err)
	}
}

func TestPostgresRoleCleanupPlanRefusesCrossDatabaseDependencies(t *testing.T) {
	requireConformance(t)
	fixture := newPostgresRoleFenceFixture(t)
	table := pgx.Identifier{"public", fixture.table}.Sanitize()
	if _, err := fixture.other.Exec(t.Context(), "CREATE TABLE "+table+" (id integer)"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.other.Exec(t.Context(), "ALTER TABLE "+table+" OWNER TO "+pgx.Identifier{fixture.role}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	tx := beginPostgresRoleFence(t, fixture.primary)
	plan, err := rolecatalog.CleanupPlan(t.Context(), tx, fixture.record(t, tx))
	if err == nil || plan != nil || !strings.Contains(err.Error(), "other-database dependencies") {
		t.Fatalf("cleanup crossed configured database boundary: %#v %v", plan, err)
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	var owner string
	if err := fixture.other.QueryRow(t.Context(), `SELECT owner.rolname FROM pg_catalog.pg_class AS relation
 JOIN pg_catalog.pg_roles AS owner ON owner.oid = relation.relowner
 WHERE relation.oid = $1::pg_catalog.regclass`, table).Scan(&owner); err != nil || owner != fixture.role {
		t.Fatalf("other-database ownership changed: %q %v", owner, err)
	}
}
