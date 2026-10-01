package rolecatalog

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/jackc/pgx/v5"
)

var ErrIdentityDrift = errors.New("managed Postgres remote identity changed; operator reconciliation is required")

const anchorQuery = `SELECT control.system_identifier::text, database.oid, database.datname,
	admin.oid, admin.rolname
FROM pg_catalog.pg_control_system() AS control
JOIN pg_catalog.pg_database AS database ON database.datname = pg_catalog.current_database()
JOIN pg_catalog.pg_roles AS admin ON admin.rolname = SESSION_USER`

// CaptureAnchor reads within the transaction returned by Begin. Requested names
// are exact; this must not trim a database/role into a different identity.
func CaptureAnchor(ctx context.Context, tx pgx.Tx, local rolejournal.Anchor) (rolejournal.Anchor, error) {
	if err := local.ValidateAuthority(); err != nil {
		return rolejournal.Anchor{}, err
	}
	if tx == nil {
		return rolejournal.Anchor{}, errors.New("managed Postgres catalog transaction is unavailable")
	}
	actual := local
	var cluster string
	if err := tx.QueryRow(ctx, anchorQuery).Scan(&cluster, &actual.DatabaseOID, &actual.DatabaseName,
		&actual.SuccessorOID, &actual.SuccessorName); err != nil {
		return rolejournal.Anchor{}, fmt.Errorf("read managed Postgres cluster/database/successor identity: %w", err)
	}
	var err error
	actual.ClusterID, err = clusterIdentifier(cluster)
	if err != nil {
		return rolejournal.Anchor{}, err
	}
	if err := actual.Validate(); err != nil {
		return rolejournal.Anchor{}, err
	}
	if actual.DatabaseName != local.DatabaseName || actual.SuccessorName != local.SuccessorName {
		return rolejournal.Anchor{}, ErrIdentityDrift
	}
	return actual, nil
}

// PostgreSQL exposes its uint64 system identifier as a signed bigint. Converting
// signed decimal to uint64 preserves the exact bits, including the high bit.
func clusterIdentifier(value string) (string, error) {
	identifier, err := strconv.ParseInt(value, 10, 64)
	if err != nil || identifier == 0 || strconv.FormatInt(identifier, 10) != value {
		return "", errors.New("invalid managed Postgres cluster system identifier")
	}
	return strconv.FormatUint(uint64(identifier), 10), nil
}

const roleQuery = `SELECT role.oid, role.rolname,
	COALESCE(pg_catalog.shobj_description(role.oid, 'pg_authid'), '')
FROM pg_catalog.pg_roles AS role WHERE role.rolname = $1`

// VerifyRole checks the complete persisted remote identity under the catalog
// fence. A missing role is drift, not proof of a completed cleanup transaction.
// The caller separately checks current local authority against the saved anchor.
func VerifyRole(ctx context.Context, tx pgx.Tx, record rolejournal.Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	if record.RoleOID == 0 {
		return errors.New("managed Postgres role identity is not bound")
	}
	actual, err := CaptureAnchor(ctx, tx, record.Intent.Anchor)
	if err != nil {
		return err
	}
	if actual != record.Intent.Anchor {
		return ErrIdentityDrift
	}
	identity, err := readRoleIdentity(ctx, tx, record.Intent.RoleName)
	if err != nil {
		return err
	}
	if identity.oid != record.RoleOID || identity.name != record.Intent.RoleName || identity.marker != record.Intent.Marker() {
		return ErrIdentityDrift
	}
	return nil
}

type roleIdentity struct {
	oid          uint32
	name, marker string
}

func readRoleIdentity(ctx context.Context, tx pgx.Tx, name string) (roleIdentity, error) {
	var identity roleIdentity
	err := tx.QueryRow(ctx, roleQuery, name).Scan(&identity.oid, &identity.name, &identity.marker)
	if errors.Is(err, pgx.ErrNoRows) {
		return roleIdentity{}, ErrIdentityDrift
	}
	if err != nil {
		return roleIdentity{}, fmt.Errorf("read managed Postgres role identity: %w", err)
	}
	return identity, nil
}
