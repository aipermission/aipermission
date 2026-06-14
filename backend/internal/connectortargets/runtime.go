package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type Resolver struct {
	db *sql.DB
}

const RuntimeCapabilityLiveConsole = "live_console"

type RuntimeSurface struct {
	ID             int64
	ConnectorKind  string
	TargetID       int64
	ProfileID      int64
	CapabilityKind string
	Label          string
	Status         TargetStatus
	CreatedAt      string
	UpdatedAt      string
}

type EnsureRuntimeSurfaceInput struct {
	ConnectorKind  string
	TargetID       int64
	ProfileID      int64
	CapabilityKind string
	Label          string
}

func NewResolver(db *sql.DB) *Resolver {
	return &Resolver{db: db}
}

func TargetProfileRef(kind string, targetID int64, profileID int64) string {
	return ConnectorTargetRef(kind, targetID, profileID)
}

func ParseTargetProfileRef(kind string, ref string) (int64, int64, bool) {
	parsedKind, targetID, profileID, ok := ParseConnectorTargetRef(ref)
	if !ok || parsedKind != kind {
		return 0, 0, false
	}
	return targetID, profileID, true
}

func (r *Resolver) ResolveActionTarget(ctx context.Context, targetRef string) (actions.ResolvedTarget, error) {
	target, profile, err := NewStore(r.db).ResolveConnectorActionTarget(ctx, targetRef)
	if err == nil {
		return actions.ResolvedTarget{Target: target, Profile: profile}, nil
	}
	if errors.Is(err, ErrInvalidTargetRef) || errors.Is(err, ErrTargetNotFound) || errors.Is(err, ErrTargetProfileNotFound) {
		return actions.ResolvedTarget{}, actions.ErrTargetNotFound
	}
	return actions.ResolvedTarget{}, err
}

func (s *Store) EnsureRuntimeSurface(ctx context.Context, input EnsureRuntimeSurfaceInput) (RuntimeSurface, error) {
	if s == nil || s.db == nil {
		return RuntimeSurface{}, fmt.Errorf("connector target store is not configured")
	}
	if !connectors.ValidIdentifier(input.ConnectorKind) {
		return RuntimeSurface{}, ValidationError("invalid connector kind")
	}
	if input.TargetID < 1 || input.ProfileID < 1 {
		return RuntimeSurface{}, ErrTargetProfileNotFound
	}
	if !connectors.ValidIdentifier(input.CapabilityKind) {
		return RuntimeSurface{}, ValidationError("invalid runtime capability kind")
	}
	label := input.Label
	if label == "" {
		label = input.CapabilityKind
	}
	now := nowString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO connector_runtime_surfaces (
			connector_kind, target_id, profile_id, capability_kind, label, status, created_at, updated_at
		)
		SELECT p.connector_kind, p.target_id, p.id, ?, ?, 'active', ?, ?
		FROM connector_credential_profiles p
		JOIN connector_targets t ON t.id = p.target_id AND t.connector_kind = p.connector_kind
		WHERE p.target_id = ? AND p.id = ? AND p.connector_kind = ? AND p.status = 'active' AND t.status = 'active'
		ON CONFLICT(connector_kind, target_id, profile_id, capability_kind) DO UPDATE SET
			label = excluded.label,
			status = 'active',
			updated_at = excluded.updated_at`,
		input.CapabilityKind,
		label,
		now,
		now,
		input.TargetID,
		input.ProfileID,
		input.ConnectorKind,
	)
	if err != nil {
		return RuntimeSurface{}, err
	}
	return s.GetRuntimeSurfaceByProfile(ctx, input.ConnectorKind, input.TargetID, input.ProfileID, input.CapabilityKind)
}

func (s *Store) GetRuntimeSurfaceByProfile(ctx context.Context, connectorKind string, targetID int64, profileID int64, capabilityKind string) (RuntimeSurface, error) {
	if s == nil || s.db == nil {
		return RuntimeSurface{}, fmt.Errorf("connector target store is not configured")
	}
	if !connectors.ValidIdentifier(connectorKind) || !connectors.ValidIdentifier(capabilityKind) || targetID < 1 || profileID < 1 {
		return RuntimeSurface{}, ErrRuntimeSurfaceNotFound
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, connector_kind, target_id, profile_id, capability_kind, label, status, created_at, updated_at
		FROM connector_runtime_surfaces
		WHERE connector_kind = ? AND target_id = ? AND profile_id = ? AND capability_kind = ? AND status = 'active'`,
		connectorKind,
		targetID,
		profileID,
		capabilityKind,
	)
	surface, err := scanRuntimeSurface(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RuntimeSurface{}, ErrRuntimeSurfaceNotFound
	}
	return surface, err
}

func (s *Store) GetRuntimeSurface(ctx context.Context, runtimeID int64) (RuntimeSurface, error) {
	if s == nil || s.db == nil {
		return RuntimeSurface{}, fmt.Errorf("connector target store is not configured")
	}
	if runtimeID < 1 {
		return RuntimeSurface{}, ErrRuntimeSurfaceNotFound
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, connector_kind, target_id, profile_id, capability_kind, label, status, created_at, updated_at
		FROM connector_runtime_surfaces
		WHERE id = ? AND status = 'active'`,
		runtimeID,
	)
	surface, err := scanRuntimeSurface(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RuntimeSurface{}, ErrRuntimeSurfaceNotFound
	}
	return surface, err
}

func (s *Store) ListRuntimeSurfacesForProfile(ctx context.Context, targetID int64, profileID int64, capabilityKind string) ([]RuntimeSurface, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("connector target store is not configured")
	}
	if targetID < 1 || profileID < 1 {
		return nil, ErrTargetProfileNotFound
	}
	args := []any{targetID, profileID}
	where := "target_id = ? AND profile_id = ? AND status = 'active'"
	if capabilityKind != "" {
		if !connectors.ValidIdentifier(capabilityKind) {
			return nil, ValidationError("invalid runtime capability kind")
		}
		where += " AND capability_kind = ?"
		args = append(args, capabilityKind)
	}
	return s.listRuntimeSurfaces(ctx, where, args...)
}

func (s *Store) ListRuntimeSurfacesForTarget(ctx context.Context, targetID int64, capabilityKind string) ([]RuntimeSurface, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("connector target store is not configured")
	}
	if targetID < 1 {
		return nil, ErrTargetNotFound
	}
	args := []any{targetID}
	where := "target_id = ? AND status = 'active'"
	if capabilityKind != "" {
		if !connectors.ValidIdentifier(capabilityKind) {
			return nil, ValidationError("invalid runtime capability kind")
		}
		where += " AND capability_kind = ?"
		args = append(args, capabilityKind)
	}
	return s.listRuntimeSurfaces(ctx, where, args...)
}

func (s *Store) TargetProfileByRuntimeID(ctx context.Context, runtimeID int64) (connectors.TargetView, connectors.CredentialProfileView, RuntimeSurface, error) {
	surface, err := s.GetRuntimeSurface(ctx, runtimeID)
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, RuntimeSurface{}, err
	}
	target, profile, err := s.ResolveConnectorActionTarget(ctx, ConnectorTargetRef(surface.ConnectorKind, surface.TargetID, surface.ProfileID))
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, RuntimeSurface{}, err
	}
	return target, profile, surface, nil
}

func (s *Store) listRuntimeSurfaces(ctx context.Context, where string, args ...any) ([]RuntimeSurface, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, connector_kind, target_id, profile_id, capability_kind, label, status, created_at, updated_at
		FROM connector_runtime_surfaces
		WHERE `+where+`
		ORDER BY target_id, profile_id, capability_kind, id`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	surfaces := []RuntimeSurface{}
	for rows.Next() {
		surface, err := scanRuntimeSurface(rows)
		if err != nil {
			return nil, err
		}
		surfaces = append(surfaces, surface)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return surfaces, nil
}

func (s *Store) ArchiveRuntimeSurfacesForTarget(ctx context.Context, targetID int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("connector target store is not configured")
	}
	if targetID < 1 {
		return ErrTargetNotFound
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE connector_runtime_surfaces
		SET status = 'archived', updated_at = ?
		WHERE target_id = ? AND status = 'active'`,
		nowString(),
		targetID,
	)
	return err
}

func (s *Store) ArchiveRuntimeSurfacesForProfile(ctx context.Context, targetID int64, profileID int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("connector target store is not configured")
	}
	if targetID < 1 || profileID < 1 {
		return ErrTargetProfileNotFound
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE connector_runtime_surfaces
		SET status = 'archived', updated_at = ?
		WHERE target_id = ? AND profile_id = ? AND status = 'active'`,
		nowString(),
		targetID,
		profileID,
	)
	return err
}

func scanRuntimeSurface(row interface {
	Scan(dest ...any) error
}) (RuntimeSurface, error) {
	var surface RuntimeSurface
	if err := row.Scan(
		&surface.ID,
		&surface.ConnectorKind,
		&surface.TargetID,
		&surface.ProfileID,
		&surface.CapabilityKind,
		&surface.Label,
		&surface.Status,
		&surface.CreatedAt,
		&surface.UpdatedAt,
	); err != nil {
		return RuntimeSurface{}, err
	}
	return surface, nil
}
