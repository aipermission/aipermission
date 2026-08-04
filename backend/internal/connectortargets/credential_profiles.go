package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type CredentialProfile struct {
	ID                  int64
	TargetID            int64
	ConnectorKind       string
	Kind                string
	Label               string
	Public              map[string]any
	EncryptedSecretJSON string
	RiskLabel           string
	CreatedAt           string
	UpdatedAt           string
}

func CredentialProfileView(profile CredentialProfile) connectors.CredentialProfileView {
	return connectors.CredentialProfileView{
		ID:             profile.ID,
		TargetID:       profile.TargetID,
		ConnectorKind:  profile.ConnectorKind,
		Kind:           profile.Kind,
		Label:          profile.Label,
		Public:         cloneMap(profile.Public),
		RiskLabel:      profile.RiskLabel,
		UpdatedAt:      profile.UpdatedAt,
		SecretRevision: secretRevision(profile.EncryptedSecretJSON),
	}
}

type CreateCredentialProfileInput struct {
	TargetID            int64
	ConnectorKind       string
	Kind                string
	Label               string
	Public              map[string]any
	EncryptedSecretJSON string
	RiskLabel           string
}

type UpdateCredentialProfileInput struct {
	TargetID            int64
	ProfileID           int64
	ConnectorKind       string
	Kind                string
	Label               string
	Public              map[string]any
	EncryptedSecretJSON *string
	RiskLabel           string
}

func (s *Store) CreateCredentialProfile(ctx context.Context, input CreateCredentialProfileInput) (CredentialProfile, error) {
	if s == nil || s.db == nil {
		return CredentialProfile{}, fmt.Errorf("connector target store is not configured")
	}
	if input.TargetID < 1 {
		return CredentialProfile{}, ValidationError("target_id is required")
	}
	if !connectors.ValidIdentifier(input.ConnectorKind) {
		return CredentialProfile{}, ValidationError("invalid connector kind")
	}
	if !connectors.ValidIdentifier(input.Kind) {
		return CredentialProfile{}, ValidationError("invalid credential kind")
	}
	label := strings.TrimSpace(input.Label)
	if label == "" {
		return CredentialProfile{}, ValidationError("profile label is required")
	}
	publicJSON, err := jsonObjectString(input.Public)
	if err != nil {
		return CredentialProfile{}, ValidationError("profile public metadata must be a JSON object")
	}
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO connector_credential_profiles (
			target_id, connector_kind, kind, label, public_json, encrypted_secret_json,
			risk_label, status, created_at, updated_at
		)
		SELECT id, ?, ?, ?, ?, ?, ?, ?, ?, ?
		FROM connector_targets
		WHERE id = ? AND connector_kind = ? AND status = 'active'`,
		input.ConnectorKind,
		input.Kind,
		label,
		publicJSON,
		input.EncryptedSecretJSON,
		strings.TrimSpace(input.RiskLabel),
		TargetStatusActive,
		now,
		now,
		input.TargetID,
		input.ConnectorKind,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return CredentialProfile{}, ValidationError("connector profile label already exists")
		}
		return CredentialProfile{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return CredentialProfile{}, err
	}
	if affected == 0 {
		return CredentialProfile{}, ErrTargetProfileNotFound
	}
	id, err := result.LastInsertId()
	if err != nil {
		return CredentialProfile{}, err
	}
	return CredentialProfile{
		ID:                  id,
		TargetID:            input.TargetID,
		ConnectorKind:       input.ConnectorKind,
		Kind:                input.Kind,
		Label:               label,
		Public:              cloneMap(input.Public),
		EncryptedSecretJSON: input.EncryptedSecretJSON,
		RiskLabel:           strings.TrimSpace(input.RiskLabel),
		CreatedAt:           now,
		UpdatedAt:           now,
	}, nil
}

func (s *Store) UpdateCredentialProfile(ctx context.Context, input UpdateCredentialProfileInput) (CredentialProfile, error) {
	if s == nil || s.db == nil {
		return CredentialProfile{}, fmt.Errorf("connector target store is not configured")
	}
	if input.TargetID < 1 || input.ProfileID < 1 {
		return CredentialProfile{}, ErrTargetProfileNotFound
	}
	if !connectors.ValidIdentifier(input.ConnectorKind) {
		return CredentialProfile{}, ValidationError("invalid connector kind")
	}
	if !connectors.ValidIdentifier(input.Kind) {
		return CredentialProfile{}, ValidationError("invalid credential kind")
	}
	label := strings.TrimSpace(input.Label)
	if label == "" {
		return CredentialProfile{}, ValidationError("profile label is required")
	}
	publicJSON, err := jsonObjectString(input.Public)
	if err != nil {
		return CredentialProfile{}, ValidationError("profile public metadata must be a JSON object")
	}
	var existingKind string
	err = s.db.QueryRowContext(ctx, `
		SELECT kind
		FROM connector_credential_profiles
		WHERE id = ? AND target_id = ? AND connector_kind = ? AND status = 'active'`,
		input.ProfileID,
		input.TargetID,
		input.ConnectorKind,
	).Scan(&existingKind)
	if errors.Is(err, sql.ErrNoRows) {
		return CredentialProfile{}, ErrTargetProfileNotFound
	}
	if err != nil {
		return CredentialProfile{}, err
	}
	if existingKind != input.Kind && input.EncryptedSecretJSON == nil {
		return CredentialProfile{}, ValidationError("credential kind change requires secret material")
	}
	now := nowString()
	var result sql.Result
	if input.EncryptedSecretJSON == nil {
		result, err = s.db.ExecContext(ctx, `
			UPDATE connector_credential_profiles
			SET connector_kind = ?, kind = ?, label = ?, public_json = ?, risk_label = ?, updated_at = ?
			WHERE id = ? AND target_id = ? AND connector_kind = ? AND status = 'active'`,
			input.ConnectorKind,
			input.Kind,
			label,
			publicJSON,
			strings.TrimSpace(input.RiskLabel),
			now,
			input.ProfileID,
			input.TargetID,
			input.ConnectorKind,
		)
	} else {
		result, err = s.db.ExecContext(ctx, `
			UPDATE connector_credential_profiles
			SET connector_kind = ?, kind = ?, label = ?, public_json = ?, encrypted_secret_json = ?, risk_label = ?, updated_at = ?
			WHERE id = ? AND target_id = ? AND connector_kind = ? AND status = 'active'`,
			input.ConnectorKind,
			input.Kind,
			label,
			publicJSON,
			*input.EncryptedSecretJSON,
			strings.TrimSpace(input.RiskLabel),
			now,
			input.ProfileID,
			input.TargetID,
			input.ConnectorKind,
		)
	}
	if err != nil {
		if isUniqueConstraintError(err) {
			return CredentialProfile{}, ValidationError("connector profile label already exists")
		}
		return CredentialProfile{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return CredentialProfile{}, err
	}
	if affected == 0 {
		return CredentialProfile{}, ErrTargetProfileNotFound
	}
	return s.GetCredentialProfile(ctx, input.TargetID, input.ProfileID)
}

func (s *Store) DeleteCredentialProfile(ctx context.Context, targetID int64, profileID int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("connector target store is not configured")
	}
	if targetID < 1 || profileID < 1 {
		return ErrTargetProfileNotFound
	}
	starter, ok := s.db.(transactionStarter)
	if !ok {
		return fmt.Errorf("connector target store cannot start transactions")
	}
	tx, err := starter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin connector profile archive: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE connector_credential_profiles
		SET status = ?, updated_at = ?
		WHERE id = ? AND target_id = ? AND status = ?`,
		TargetStatusArchived,
		nowString(),
		profileID,
		targetID,
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
		return ErrTargetProfileNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE connector_runtime_surfaces
		SET status = ?, updated_at = ?
		WHERE target_id = ? AND profile_id = ? AND status = ?`,
		TargetStatusArchived,
		nowString(),
		targetID,
		profileID,
		TargetStatusActive,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM token_connector_action_permissions
		WHERE target_id = ? AND profile_id = ?`,
		targetID,
		profileID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListCredentialProfiles(ctx context.Context, targetID int64) ([]CredentialProfile, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("connector target store is not configured")
	}
	if targetID < 1 {
		return nil, ErrTargetProfileNotFound
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			p.id, p.target_id, p.connector_kind, p.kind, p.label, p.public_json,
			p.encrypted_secret_json, p.risk_label, p.created_at, p.updated_at
		FROM connector_credential_profiles p
		JOIN connector_targets t ON t.id = p.target_id
			WHERE p.target_id = ? AND p.connector_kind = t.connector_kind AND t.status = 'active' AND p.status = 'active'
		ORDER BY p.label, p.id`,
		targetID,
	)
	if err != nil {
		return nil, fmt.Errorf("list connector credential profiles: %w", err)
	}
	defer rows.Close()

	profiles := []CredentialProfile{}
	for rows.Next() {
		profile, err := scanCredentialProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connector credential profiles: %w", err)
	}
	return profiles, nil
}

func (s *Store) GetCredentialProfile(ctx context.Context, targetID int64, profileID int64) (CredentialProfile, error) {
	if s == nil || s.db == nil {
		return CredentialProfile{}, fmt.Errorf("connector target store is not configured")
	}
	if targetID < 1 || profileID < 1 {
		return CredentialProfile{}, ErrTargetProfileNotFound
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT
			p.id, p.target_id, p.connector_kind, p.kind, p.label, p.public_json,
			p.encrypted_secret_json, p.risk_label, p.created_at, p.updated_at
		FROM connector_credential_profiles p
		JOIN connector_targets t ON t.id = p.target_id
			WHERE p.target_id = ? AND p.id = ? AND p.connector_kind = t.connector_kind AND t.status = 'active' AND p.status = 'active'`,
		targetID,
		profileID,
	)
	profile, err := scanCredentialProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CredentialProfile{}, ErrTargetProfileNotFound
	}
	if err != nil {
		return CredentialProfile{}, err
	}
	return profile, nil
}
