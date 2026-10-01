package rolecatalog

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/jackc/pgx/v5"
)

// CreatedRoleOID binds an uncommitted role to its previously persisted intent.
// Callers must persist this exact OID before issuing remote COMMIT.
func CreatedRoleOID(ctx context.Context, tx pgx.Tx, entry rolejournal.Entry) (uint32, error) {
	if err := entry.Record.Validate(); err != nil {
		return 0, err
	}
	if entry.ResourceID < 1 || entry.Record.Status != rolejournal.ProvisionIntent || entry.Record.RoleOID != 0 {
		return 0, errors.New("managed Postgres creation intent is not unbound")
	}
	anchor, err := CaptureAnchor(ctx, tx, entry.Record.Intent.Anchor)
	if err != nil {
		return 0, err
	}
	if anchor != entry.Record.Intent.Anchor {
		return 0, ErrIdentityDrift
	}
	identity, err := readRoleIdentity(ctx, tx, entry.Record.Intent.RoleName)
	if err != nil {
		return 0, err
	}
	if identity.oid == 0 || identity.oid == anchor.SuccessorOID || identity.name != entry.Record.Intent.RoleName ||
		identity.marker != entry.Record.Intent.Marker() {
		return 0, ErrIdentityDrift
	}
	return identity.oid, nil
}
