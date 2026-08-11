package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type TargetStatus string

const (
	TargetStatusActive   TargetStatus = "active"
	TargetStatusArchived TargetStatus = "archived"
)

type Target struct {
	ID            int64
	ProjectID     int64
	ProjectName   string
	ProjectSlug   string
	ConnectorKind string
	Name          string
	Config        map[string]any
	Status        TargetStatus
	CreatedAt     string
	UpdatedAt     string
}

type ListTargetsFilter struct {
	ConnectorKind string
	ProjectID     int64
}

type CreateTargetInput struct {
	ProjectID     int64
	ConnectorKind string
	Name          string
	Config        map[string]any
}

type UpdateTargetInput struct {
	ID        int64
	ProjectID int64
	Name      string
	Config    map[string]any
}

func (s *Store) CreateTarget(ctx context.Context, input CreateTargetInput) (Target, error) {
	if s == nil || s.db == nil {
		return Target{}, fmt.Errorf("connector target store is not configured")
	}
	if !connectors.ValidIdentifier(input.ConnectorKind) {
		return Target{}, ValidationError("invalid connector kind")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Target{}, ValidationError("target name is required")
	}
	configJSON, err := jsonObjectString(input.Config)
	if err != nil {
		return Target{}, ValidationError("target config must be a JSON object")
	}
	projectID, err := s.resolveProjectID(ctx, input.ProjectID)
	if err != nil {
		return Target{}, err
	}
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO connector_targets (project_id, connector_kind, name, config_json, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'active', ?, ?)`,
		projectID,
		input.ConnectorKind,
		name,
		configJSON,
		now,
		now,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return Target{}, ValidationError("connector target name already exists")
		}
		return Target{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Target{}, err
	}
	return s.GetTarget(ctx, id)
}

func (s *Store) UpdateTarget(ctx context.Context, input UpdateTargetInput) (Target, error) {
	if s == nil || s.db == nil {
		return Target{}, fmt.Errorf("connector target store is not configured")
	}
	if input.ID < 1 {
		return Target{}, ErrTargetNotFound
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Target{}, ValidationError("target name is required")
	}
	configJSON, err := jsonObjectString(input.Config)
	if err != nil {
		return Target{}, ValidationError("target config must be a JSON object")
	}
	projectID, err := s.resolveProjectID(ctx, input.ProjectID)
	if err != nil {
		return Target{}, err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE connector_targets
		SET project_id = ?, name = ?, config_json = ?, updated_at = ?
		WHERE id = ? AND status = 'active'`,
		projectID,
		name,
		configJSON,
		nowString(),
		input.ID,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return Target{}, ValidationError("connector target name already exists")
		}
		return Target{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Target{}, err
	}
	if affected == 0 {
		return Target{}, ErrTargetNotFound
	}
	return s.GetTarget(ctx, input.ID)
}

func (s *Store) DeleteTarget(ctx context.Context, id int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("connector target store is not configured")
	}
	if id < 1 {
		return ErrTargetNotFound
	}
	tx, commit, rollback, err := s.transaction(ctx, "connector target archive")
	if err != nil {
		return fmt.Errorf("begin connector target archive: %w", err)
	}
	defer rollback()
	now := nowString()
	result, err := tx.ExecContext(ctx, `
		UPDATE connector_targets
		SET status = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		TargetStatusArchived,
		now,
		id,
		TargetStatusActive,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrTargetNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE connector_credential_profiles
		SET status = ?, updated_at = ?
		WHERE target_id = ? AND status = ?`,
		TargetStatusArchived,
		now,
		id,
		TargetStatusActive,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE connector_runtime_surfaces
		SET status = ?, updated_at = ?
		WHERE target_id = ? AND status = ?`,
		TargetStatusArchived,
		now,
		id,
		TargetStatusActive,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM token_connector_action_permissions
		WHERE target_id = ?`,
		id,
	); err != nil {
		return err
	}
	return commit()
}

func (s *Store) ListTargets(ctx context.Context, filter ListTargetsFilter) ([]Target, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("connector target store is not configured")
	}
	args := []any{}
	where := "t.status = 'active'"
	if strings.TrimSpace(filter.ConnectorKind) != "" {
		if !connectors.ValidIdentifier(filter.ConnectorKind) {
			return nil, ValidationError("invalid connector kind")
		}
		where += " AND t.connector_kind = ?"
		args = append(args, filter.ConnectorKind)
	}
	if filter.ProjectID != 0 {
		if filter.ProjectID < 1 {
			return nil, ValidationError("invalid project id")
		}
		where += " AND t.project_id = ?"
		args = append(args, filter.ProjectID)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.project_id, p.name, p.slug, t.connector_kind, t.name, t.config_json, t.status, t.created_at, t.updated_at
		FROM connector_targets t
		JOIN projects p ON p.id = t.project_id AND p.status = 'active'
		WHERE `+where+`
		ORDER BY lower(p.name), t.connector_kind, lower(t.name), t.id`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list connector targets: %w", err)
	}
	defer rows.Close()

	targets := []Target{}
	for rows.Next() {
		target, err := scanTarget(rows)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connector targets: %w", err)
	}
	return targets, nil
}

func (s *Store) GetTarget(ctx context.Context, id int64) (Target, error) {
	if s == nil || s.db == nil {
		return Target{}, fmt.Errorf("connector target store is not configured")
	}
	if id < 1 {
		return Target{}, ErrTargetNotFound
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT t.id, t.project_id, p.name, p.slug, t.connector_kind, t.name, t.config_json, t.status, t.created_at, t.updated_at
		FROM connector_targets t
		JOIN projects p ON p.id = t.project_id AND p.status = 'active'
		WHERE t.id = ? AND t.status = 'active'`,
		id,
	)
	target, err := scanTarget(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Target{}, ErrTargetNotFound
	}
	if err != nil {
		return Target{}, err
	}
	return target, nil
}
