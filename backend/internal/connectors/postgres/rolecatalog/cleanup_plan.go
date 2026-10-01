package rolecatalog

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/jackc/pgx/v5"
)

const maxCleanupStatements = 10000
const maxCleanupStatementBytes = 1 << 20

// Shared ownership and other-database dependencies are outside the configured
// database's authority. REASSIGN OWNED would otherwise also affect shared objects.
const cleanupScopeQuery = `SELECT EXISTS (
 SELECT 1 FROM pg_catalog.pg_shdepend AS dependency
 WHERE dependency.refclassid = 'pg_catalog.pg_authid'::pg_catalog.regclass
 AND dependency.refobjid = $1 AND (
  (dependency.dbid <> 0 AND dependency.dbid <> $2)
  OR (dependency.dbid = 0 AND dependency.deptype = 'o')
  OR (dependency.dbid = 0 AND dependency.classid = 'pg_catalog.pg_database'::pg_catalog.regclass
      AND dependency.objid <> $2)))`

// CleanupPlan reads under Begin's identity fence and returns only reassignment,
// privilege revocation and DROP ROLE. It NEVER uses DROP OWNED: a dependency
// writer can commit new ownership after reassignment, which DROP OWNED could
// destroy. DROP ROLE instead rejects surviving ownership/dependencies atomically.
// Callers must execute this whole plan in the same transaction, with bounded
// waits, and confirm cleanup only after acknowledged COMMIT.
func CleanupPlan(ctx context.Context, tx pgx.Tx, record rolejournal.Record) ([]string, error) {
	if err := VerifyRole(ctx, tx, record); err != nil {
		return nil, err
	}
	if err := lockSharedOwnershipRows(ctx, tx); err != nil {
		return nil, err
	}
	var outsideScope bool
	if err := tx.QueryRow(ctx, cleanupScopeQuery, record.RoleOID, record.Intent.Anchor.DatabaseOID).Scan(&outsideScope); err != nil {
		return nil, fmt.Errorf("read managed Postgres cleanup scope: %w", err)
	}
	if outsideScope {
		return nil, errors.New("managed Postgres role has shared ownership or other-database dependencies; operator reconciliation is required")
	}
	rows, err := tx.Query(ctx, cleanupPrivilegesQuery, record.RoleOID, record.Intent.Anchor.DatabaseOID)
	if err != nil {
		return nil, fmt.Errorf("read managed Postgres privilege revocation plan: %w", err)
	}
	if rows == nil {
		return nil, errors.New("managed Postgres privilege revocation rows are unavailable")
	}
	defer rows.Close()
	role := pgx.Identifier{record.Intent.RoleName}.Sanitize()
	statements := []string{fmt.Sprintf("REASSIGN OWNED BY %s TO %s", role, pgx.Identifier{record.Intent.Anchor.SuccessorName}.Sanitize())}
	drop := "DROP ROLE " + role
	bytes := len(statements[0]) + len(drop)
	seen := map[string]bool{}
	rowsRead := 0
	for rows.Next() {
		rowsRead++
		if rowsRead > maxCleanupStatements {
			return nil, errors.New("managed Postgres privilege target count exceeds its budget")
		}
		var kind, schema, name string
		if err := rows.Scan(&kind, &schema, &name); err != nil {
			return nil, fmt.Errorf("read managed Postgres privilege target: %w", err)
		}
		statement, err := revokeStatement(kind, schema, name, role)
		if err != nil {
			return nil, err
		}
		if seen[statement] {
			continue
		}
		seen[statement] = true
		bytes += len(statement)
		if len(statements) >= maxCleanupStatements-1 || bytes > maxCleanupStatementBytes {
			return nil, errors.New("managed Postgres privilege revocation plan exceeds its budget")
		}
		statements = append(statements, statement)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read managed Postgres privilege targets: %w", err)
	}
	return append(statements, drop), nil
}

func revokeStatement(kind, schema, name, role string) (string, error) {
	if !catalogIdentifier(name) || (schema != "" && !catalogIdentifier(schema)) {
		return "", errors.New("invalid managed Postgres privilege target")
	}
	var target string
	switch kind {
	case "database", "schema", "language", "foreign data wrapper", "foreign server":
		if schema != "" {
			return "", errors.New("unexpected managed Postgres privilege schema")
		}
		target = kind + " " + pgx.Identifier{name}.Sanitize()
	case "table", "sequence", "type":
		if schema == "" {
			return "", errors.New("managed Postgres privilege schema is missing")
		}
		target = kind + " " + pgx.Identifier{schema, name}.Sanitize()
	case "routines":
		if schema != "" {
			return "", errors.New("unexpected managed Postgres routine schema")
		}
		target = "ALL ROUTINES IN SCHEMA " + pgx.Identifier{name}.Sanitize()
	case "large object":
		oid, err := strconv.ParseUint(name, 10, 32)
		if err != nil || oid == 0 || strconv.FormatUint(oid, 10) != name || schema != "" {
			return "", errors.New("invalid managed Postgres large object identity")
		}
		target = "LARGE OBJECT " + name
	default:
		return "", errors.New("unsupported managed Postgres privilege target")
	}
	return fmt.Sprintf("REVOKE ALL PRIVILEGES ON %s FROM %s CASCADE", target, role), nil
}

func catalogIdentifier(value string) bool {
	return value != "" && len(value) <= 63 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}
