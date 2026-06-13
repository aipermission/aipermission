package connectortargets

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const connectorTargetRefSeparator = ":"

type TargetStatus string

const (
	TargetStatusActive   TargetStatus = "active"
	TargetStatusArchived TargetStatus = "archived"
)

type ActionPermissionRule string

const (
	ActionPermissionAlwaysRun        ActionPermissionRule = "always_run"
	ActionPermissionApprovalRequired ActionPermissionRule = "approval_required"
	ActionPermissionBlocked          ActionPermissionRule = "blocked"
)

type Store struct {
	db *sql.DB
}

type ValidationError string

func (e ValidationError) Error() string {
	return string(e)
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

type Target struct {
	ID            int64
	ConnectorKind string
	Name          string
	Config        map[string]any
	Status        TargetStatus
	CreatedAt     string
	UpdatedAt     string
}

type ListTargetsFilter struct {
	ConnectorKind string
}

type CreateTargetInput struct {
	ConnectorKind string
	Name          string
	Config        map[string]any
}

type UpdateTargetInput struct {
	ID     int64
	Name   string
	Config map[string]any
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
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO connector_targets (connector_kind, name, config_json, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`,
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
	return Target{
		ID:            id,
		ConnectorKind: input.ConnectorKind,
		Name:          name,
		Config:        cloneMap(input.Config),
		Status:        TargetStatusActive,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
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
	result, err := s.db.ExecContext(ctx, `
		UPDATE connector_targets
		SET name = ?, config_json = ?, updated_at = ?
		WHERE id = ? AND status = 'active'`,
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin connector target archive: %w", err)
	}
	defer tx.Rollback()
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
	return tx.Commit()
}

func (s *Store) ListTargets(ctx context.Context, filter ListTargetsFilter) ([]Target, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("connector target store is not configured")
	}
	args := []any{}
	where := "status = 'active'"
	if strings.TrimSpace(filter.ConnectorKind) != "" {
		if !connectors.ValidIdentifier(filter.ConnectorKind) {
			return nil, ValidationError("invalid connector kind")
		}
		where += " AND connector_kind = ?"
		args = append(args, filter.ConnectorKind)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, connector_kind, name, config_json, status, created_at, updated_at
		FROM connector_targets
		WHERE `+where+`
		ORDER BY connector_kind, name, id`,
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
		SELECT id, connector_kind, name, config_json, status, created_at, updated_at
		FROM connector_targets
		WHERE id = ? AND status = 'active'`,
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
	result, err := s.db.ExecContext(ctx, `
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
	return nil
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

type SetActionPermissionInput struct {
	TokenID       int64
	TargetID      int64
	ProfileID     int64
	ActionName    string
	ExecutionRule ActionPermissionRule
	ExpiresAt     *time.Time
}

type ActionPermission struct {
	TokenID       int64
	TargetID      int64
	TargetName    string
	ProfileID     int64
	ProfileLabel  string
	ConnectorKind string
	ProfileKind   string
	ActionName    string
	ExecutionRule ActionPermissionRule
	ExpiresAt     string
	CreatedAt     string
	UpdatedAt     string
}

type ActionRequest struct {
	ID                   int64
	TokenID              *int64
	TokenName            string
	TargetID             int64
	TargetName           string
	ProfileID            int64
	ProfileLabel         string
	ConnectorKind        string
	ActionName           string
	Source               string
	Input                map[string]any
	EncryptedPayloadJSON string
	Reason               string
	Status               connectors.ResultStatus
	Output               any
	DisplayText          string
	Error                string
	ApprovalContext      string
	ApprovalContextHash  string
	ApprovalContextDrift string
	CreatedAt            string
	CompletedAt          *string
}

type InsertActionRequestInput struct {
	TokenID              *int64
	TargetID             int64
	ProfileID            int64
	ConnectorKind        string
	ActionName           string
	Source               string
	Input                map[string]any
	EncryptedPayloadJSON string
	Reason               string
	Status               connectors.ResultStatus
	ApprovalContext      string
	ApprovalContextHash  string
}

type FinishActionRequestInput struct {
	ID              int64
	Status          connectors.ResultStatus
	Output          any
	DisplayText     string
	Error           string
	AllowedStatuses []connectors.ResultStatus
}

type StaleActionRequestsForTargetInput struct {
	TargetID  int64
	ProfileID int64
	Error     string
}

type StaleActionRequestsForTargetResult struct {
	IDs      []int64
	Affected int64
}

type ActionRequestFilter struct {
	Status string
	Limit  int
}

func (s *Store) SetActionPermission(ctx context.Context, input SetActionPermissionInput) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("connector target store is not configured")
	}
	if err := validateActionPermissionInput(input); err != nil {
		return err
	}
	// This store owns target/profile existence only. Action-catalog validation
	// belongs to the API/service layer because supported actions can depend on
	// connector metadata and target/profile public configuration.
	if err := s.requireActiveTargetProfile(ctx, input.TargetID, input.ProfileID); err != nil {
		return err
	}
	now := nowString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO token_connector_action_permissions (
			token_id, target_id, profile_id, action_name, execution_rule, expires_at,
			created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(token_id, target_id, profile_id, action_name) DO UPDATE SET
			execution_rule = excluded.execution_rule,
			expires_at = excluded.expires_at,
			updated_at = excluded.updated_at`,
		input.TokenID,
		input.TargetID,
		input.ProfileID,
		input.ActionName,
		input.ExecutionRule,
		actionPermissionExpiresAt(input),
		now,
		now,
	)
	return err
}

func (s *Store) GetActionPermission(ctx context.Context, tokenID int64, targetID int64, profileID int64, actionName string, now time.Time) (ActionPermission, error) {
	if s == nil || s.db == nil {
		return ActionPermission{}, fmt.Errorf("connector target store is not configured")
	}
	if tokenID < 1 || targetID < 1 || profileID < 1 || !connectors.ValidIdentifier(actionName) {
		return ActionPermission{}, ErrActionPermissionNotFound
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT
			p.token_id, p.target_id, t.name, p.profile_id, cp.label,
			t.connector_kind, cp.kind, p.action_name, p.execution_rule,
			COALESCE(p.expires_at, ''), p.created_at, p.updated_at
		FROM token_connector_action_permissions p
		JOIN connector_targets t ON t.id = p.target_id
		JOIN connector_credential_profiles cp ON cp.id = p.profile_id AND cp.target_id = p.target_id
		WHERE
			p.token_id = ?
			AND p.target_id = ?
				AND p.profile_id = ?
				AND p.action_name = ?
				AND t.status = 'active'
				AND cp.status = 'active'
				AND cp.connector_kind = t.connector_kind
				AND (COALESCE(p.expires_at, '') = '' OR p.expires_at > ?)`,
		tokenID,
		targetID,
		profileID,
		actionName,
		now.UTC().Format(time.RFC3339),
	)
	permission, err := scanActionPermission(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ActionPermission{}, ErrActionPermissionNotFound
	}
	if err != nil {
		return ActionPermission{}, err
	}
	return permission, nil
}

func (s *Store) ReplaceActionPermissions(ctx context.Context, tokenID int64, inputs []SetActionPermissionInput) ([]ActionPermission, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("connector target store is not configured")
	}
	if tokenID < 1 {
		return nil, ValidationError("token_id is required")
	}
	if err := s.validateActionPermissions(ctx, tokenID, inputs); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin connector permission update: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM token_connector_action_permissions WHERE token_id = ?`, tokenID); err != nil {
		return nil, fmt.Errorf("clear connector action permissions: %w", err)
	}
	now := nowString()
	for _, input := range inputs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO token_connector_action_permissions (
				token_id, target_id, profile_id, action_name, execution_rule, expires_at,
				created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			tokenID,
			input.TargetID,
			input.ProfileID,
			input.ActionName,
			input.ExecutionRule,
			actionPermissionExpiresAt(input),
			now,
			now,
		); err != nil {
			return nil, fmt.Errorf("insert connector action permission: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit connector permission update: %w", err)
	}
	return s.ListActionPermissions(ctx, tokenID)
}

func (s *Store) ListActionPermissions(ctx context.Context, tokenID int64) ([]ActionPermission, error) {
	return s.ListActiveActionPermissions(ctx, tokenID, time.Now().UTC())
}

func (s *Store) ListActiveActionPermissions(ctx context.Context, tokenID int64, now time.Time) ([]ActionPermission, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("connector target store is not configured")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			p.token_id, p.target_id, t.name, p.profile_id, cp.label,
			t.connector_kind, cp.kind, p.action_name, p.execution_rule,
			COALESCE(p.expires_at, ''), p.created_at, p.updated_at
		FROM token_connector_action_permissions p
		JOIN connector_targets t ON t.id = p.target_id
		JOIN connector_credential_profiles cp ON cp.id = p.profile_id AND cp.target_id = p.target_id
			WHERE
				p.token_id = ?
				AND t.status = 'active'
				AND cp.status = 'active'
				AND cp.connector_kind = t.connector_kind
				AND (COALESCE(p.expires_at, '') = '' OR p.expires_at > ?)
		ORDER BY t.connector_kind, t.name, cp.label, p.action_name`,
		tokenID,
		now.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("list connector action permissions: %w", err)
	}
	defer rows.Close()

	permissions := []ActionPermission{}
	for rows.Next() {
		item, err := scanActionPermission(rows)
		if err != nil {
			return nil, err
		}
		permissions = append(permissions, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connector action permissions: %w", err)
	}
	return permissions, nil
}

func (s *Store) InsertActionRequest(ctx context.Context, input InsertActionRequestInput) (ActionRequest, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, fmt.Errorf("connector target store is not configured")
	}
	if err := validateActionRequestInput(input); err != nil {
		return ActionRequest{}, err
	}
	inputJSON, err := jsonObjectString(input.Input)
	if err != nil {
		return ActionRequest{}, ValidationError("action input must be a JSON object")
	}
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO connector_action_requests (
			token_id, target_id, profile_id, connector_kind, action_name, source, input_json,
			encrypted_payload_json, reason, status, approval_context,
			approval_context_hash, created_at
		)
		SELECT ?, t.id, p.id, t.connector_kind, ?, ?, ?, ?, ?, ?, ?, ?, ?
		FROM connector_targets t
		JOIN connector_credential_profiles p ON p.target_id = t.id
		WHERE
			t.id = ?
				AND p.id = ?
				AND t.connector_kind = ?
				AND p.connector_kind = t.connector_kind
				AND t.status = 'active'
				AND p.status = 'active'`,
		nullableInt64(input.TokenID),
		input.ActionName,
		actionRequestSource(input.Source),
		inputJSON,
		strings.TrimSpace(input.EncryptedPayloadJSON),
		strings.TrimSpace(input.Reason),
		string(input.Status),
		strings.TrimSpace(input.ApprovalContext),
		strings.TrimSpace(input.ApprovalContextHash),
		now,
		input.TargetID,
		input.ProfileID,
		input.ConnectorKind,
	)
	if err != nil {
		return ActionRequest{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ActionRequest{}, err
	}
	if affected == 0 {
		return ActionRequest{}, ErrTargetProfileNotFound
	}
	id, err := result.LastInsertId()
	if err != nil {
		return ActionRequest{}, err
	}
	return s.GetActionRequest(ctx, id)
}

func (s *Store) FinishActionRequest(ctx context.Context, input FinishActionRequestInput) (ActionRequest, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, fmt.Errorf("connector target store is not configured")
	}
	if input.ID < 1 {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	if !validActionRequestTerminalStatus(input.Status) {
		return ActionRequest{}, ValidationError("invalid terminal action request status")
	}
	outputJSON, err := jsonValueString(input.Output)
	if err != nil {
		return ActionRequest{}, ValidationError("action output must be valid JSON")
	}
	allowedStatuses, err := finishAllowedStatuses(input.AllowedStatuses)
	if err != nil {
		return ActionRequest{}, err
	}
	statusPlaceholders := strings.TrimRight(strings.Repeat("?,", len(allowedStatuses)), ",")
	now := nowString()
	args := []any{
		string(input.Status),
		outputJSON,
		strings.TrimSpace(input.DisplayText),
		strings.TrimSpace(input.Error),
		now,
		input.ID,
	}
	for _, status := range allowedStatuses {
		args = append(args, string(status))
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE connector_action_requests
		SET status = ?, output_json = ?, display_text = ?, error = ?, completed_at = ?
		WHERE id = ? AND status IN (`+statusPlaceholders+`)`,
		args...,
	)
	if err != nil {
		return ActionRequest{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ActionRequest{}, err
	}
	if affected == 0 {
		return s.GetActionRequest(ctx, input.ID)
	}
	return s.GetActionRequest(ctx, input.ID)
}

func (s *Store) StaleActionRequestsForTarget(ctx context.Context, input StaleActionRequestsForTargetInput) (StaleActionRequestsForTargetResult, error) {
	if s == nil || s.db == nil {
		return StaleActionRequestsForTargetResult{}, fmt.Errorf("connector target store is not configured")
	}
	if input.TargetID < 1 {
		return StaleActionRequestsForTargetResult{}, ErrTargetNotFound
	}
	if input.ProfileID < 0 {
		return StaleActionRequestsForTargetResult{}, ErrTargetProfileNotFound
	}
	where := "target_id = ? AND status IN (?, ?)"
	args := []any{input.TargetID, string(connectors.ResultApprovalPending), string(connectors.ResultRunning)}
	if input.ProfileID > 0 {
		where += " AND profile_id = ?"
		args = append(args, input.ProfileID)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM connector_action_requests WHERE `+where, args...)
	if err != nil {
		return StaleActionRequestsForTargetResult{}, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return StaleActionRequestsForTargetResult{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return StaleActionRequestsForTargetResult{}, err
	}
	if len(ids) == 0 {
		return StaleActionRequestsForTargetResult{}, nil
	}
	updateArgs := []any{string(connectors.ResultStale), strings.TrimSpace(input.Error), nowString()}
	updateArgs = append(updateArgs, args...)
	result, err := s.db.ExecContext(ctx, `
		UPDATE connector_action_requests
		SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?)
		WHERE `+where,
		updateArgs...,
	)
	if err != nil {
		return StaleActionRequestsForTargetResult{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		affected = int64(len(ids))
	}
	return StaleActionRequestsForTargetResult{IDs: ids, Affected: affected}, nil
}

func (s *Store) MarkActionRequestRunning(ctx context.Context, id int64) (ActionRequest, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, fmt.Errorf("connector target store is not configured")
	}
	if id < 1 {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE connector_action_requests
		SET status = ?, error = ''
		WHERE id = ? AND status = ?`,
		string(connectors.ResultRunning),
		id,
		string(connectors.ResultApprovalPending),
	)
	if err != nil {
		return ActionRequest{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ActionRequest{}, err
	}
	if affected == 0 {
		return ActionRequest{}, ErrActionRequestNotPending
	}
	return s.GetActionRequest(ctx, id)
}

func (s *Store) DeclineActionRequest(ctx context.Context, id int64, message string) (ActionRequest, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, fmt.Errorf("connector target store is not configured")
	}
	if id < 1 {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE connector_action_requests
		SET status = ?, error = ?, completed_at = ?
		WHERE id = ? AND status = ?`,
		string(connectors.ResultDeclined),
		strings.TrimSpace(message),
		now,
		id,
		string(connectors.ResultApprovalPending),
	)
	if err != nil {
		return ActionRequest{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ActionRequest{}, err
	}
	if affected == 0 {
		return ActionRequest{}, ErrActionRequestNotPending
	}
	return s.GetActionRequest(ctx, id)
}

func (s *Store) GetActionRequest(ctx context.Context, id int64) (ActionRequest, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, fmt.Errorf("connector target store is not configured")
	}
	if id < 1 {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	row := s.db.QueryRowContext(ctx, actionRequestSelectSQL()+` WHERE r.id = ?`, id)
	request, err := scanActionRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	if err != nil {
		return ActionRequest{}, err
	}
	return request, nil
}

func (s *Store) ListActionRequests(ctx context.Context, filter ActionRequestFilter) ([]ActionRequest, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("connector target store is not configured")
	}
	where := []string{"(? = '' OR r.status = ?)"}
	args := []any{strings.TrimSpace(filter.Status), strings.TrimSpace(filter.Status)}
	limit := filter.Limit
	if limit < 1 || limit > 100 {
		limit = 100
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, actionRequestSelectSQL()+`
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list connector action requests: %w", err)
	}
	defer rows.Close()
	items := []ActionRequest{}
	for rows.Next() {
		item, err := scanActionRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connector action requests: %w", err)
	}
	return items, nil
}

func (s *Store) validateActionPermissions(ctx context.Context, tokenID int64, inputs []SetActionPermissionInput) error {
	seen := map[string]bool{}
	for _, input := range inputs {
		input.TokenID = tokenID
		if err := validateActionPermissionInput(input); err != nil {
			return err
		}
		key := fmt.Sprintf("%d:%d:%s", input.TargetID, input.ProfileID, input.ActionName)
		if seen[key] {
			return ValidationError("connector action permissions must be unique per target, profile, and action")
		}
		seen[key] = true
		var exists int
		err := s.db.QueryRowContext(ctx, activeTargetProfileSQL(), input.TargetID, input.ProfileID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return ValidationError("connector target/profile does not exist")
		}
		if err != nil {
			return fmt.Errorf("validate connector target/profile: %w", err)
		}
	}
	return nil
}

func (s *Store) requireActiveTargetProfile(ctx context.Context, targetID int64, profileID int64) error {
	var exists int
	err := s.db.QueryRowContext(ctx, activeTargetProfileSQL(), targetID, profileID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTargetProfileNotFound
	}
	if err != nil {
		return fmt.Errorf("validate connector target/profile: %w", err)
	}
	return nil
}

func activeTargetProfileSQL() string {
	return `
		SELECT 1
		FROM connector_targets t
		JOIN connector_credential_profiles p ON p.target_id = t.id
		WHERE t.id = ? AND p.id = ? AND p.connector_kind = t.connector_kind AND t.status = 'active' AND p.status = 'active'`
}

func validateActionPermissionInput(input SetActionPermissionInput) error {
	if input.TokenID < 1 || input.TargetID < 1 || input.ProfileID < 1 {
		return ValidationError("token_id, target_id, and profile_id are required")
	}
	if !connectors.ValidIdentifier(input.ActionName) {
		return ValidationError("invalid action name")
	}
	switch input.ExecutionRule {
	case ActionPermissionAlwaysRun, ActionPermissionApprovalRequired, ActionPermissionBlocked:
	default:
		return ValidationError("invalid execution rule")
	}
	if input.ExecutionRule == ActionPermissionBlocked && input.ExpiresAt != nil {
		return ValidationError("expires_at is not supported for blocked permissions")
	}
	return nil
}

func actionPermissionExpiresAt(input SetActionPermissionInput) any {
	if input.ExpiresAt == nil {
		return nil
	}
	return input.ExpiresAt.UTC().Format(time.RFC3339)
}

func ConnectorTargetRef(connectorKind string, targetID int64, profileID int64) string {
	return fmt.Sprintf("%s%s%d%s%d", connectorKind, connectorTargetRefSeparator, targetID, connectorTargetRefSeparator, profileID)
}

func ParseConnectorTargetRef(ref string) (string, int64, int64, bool) {
	parts := strings.Split(strings.TrimSpace(ref), connectorTargetRefSeparator)
	if len(parts) != 3 || !connectors.ValidIdentifier(parts[0]) {
		return "", 0, 0, false
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || targetID < 1 {
		return "", 0, 0, false
	}
	profileID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || profileID < 1 {
		return "", 0, 0, false
	}
	return parts[0], targetID, profileID, true
}

func (s *Store) ResolveConnectorActionTarget(ctx context.Context, targetRef string) (connectors.TargetView, connectors.CredentialProfileView, error) {
	if s == nil || s.db == nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, fmt.Errorf("connector target store is not configured")
	}
	connectorKind, targetID, profileID, ok := ParseConnectorTargetRef(targetRef)
	if !ok {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, ErrInvalidTargetRef
	}
	var targetConfigJSON string
	var target connectors.TargetView
	var profile CredentialProfile
	var profilePublicJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT
			t.id, t.connector_kind, t.name, t.config_json,
			p.id, p.target_id, p.connector_kind, p.kind, p.label, p.public_json,
			p.encrypted_secret_json, p.risk_label, p.updated_at
		FROM connector_targets t
		JOIN connector_credential_profiles p ON p.target_id = t.id
		WHERE
			t.id = ?
				AND p.id = ?
				AND t.connector_kind = ?
				AND p.connector_kind = t.connector_kind
				AND t.status = 'active'
				AND p.status = 'active'`,
		targetID,
		profileID,
		connectorKind,
	).Scan(
		&target.ID,
		&target.ConnectorKind,
		&target.Name,
		&targetConfigJSON,
		&profile.ID,
		&profile.TargetID,
		&profile.ConnectorKind,
		&profile.Kind,
		&profile.Label,
		&profilePublicJSON,
		&profile.EncryptedSecretJSON,
		&profile.RiskLabel,
		&profile.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, ErrTargetProfileNotFound
	}
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, err
	}
	target.Ref = ConnectorTargetRef(target.ConnectorKind, target.ID, profile.ID)
	target.Config, err = parseJSONObject(targetConfigJSON)
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, fmt.Errorf("decode target config: %w", err)
	}
	profile.Public, err = parseJSONObject(profilePublicJSON)
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, fmt.Errorf("decode profile public metadata: %w", err)
	}
	return target, CredentialProfileView(profile), nil
}

var (
	ErrInvalidTargetRef         = errors.New("invalid connector target ref")
	ErrTargetNotFound           = errors.New("connector target not found")
	ErrTargetProfileNotFound    = errors.New("connector target profile not found")
	ErrActionPermissionNotFound = errors.New("connector action permission not found")
	ErrActionRequestNotFound    = errors.New("connector action request not found")
	ErrActionRequestNotPending  = errors.New("connector action request is not pending")
)

func jsonObjectString(value map[string]any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if !json.Valid(encoded) || len(encoded) == 0 || encoded[0] != '{' {
		return "", fmt.Errorf("value must be a JSON object")
	}
	return string(encoded), nil
}

func jsonValueString(value any) (string, error) {
	if value == nil {
		return "null", nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if !json.Valid(encoded) {
		return "", fmt.Errorf("value must be valid JSON")
	}
	return string(encoded), nil
}

func secretRevision(encryptedSecretJSON string) string {
	if strings.TrimSpace(encryptedSecretJSON) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(encryptedSecretJSON))
	return hex.EncodeToString(sum[:])
}

func parseJSONObject(value string) (map[string]any, error) {
	if strings.TrimSpace(value) == "" {
		return map[string]any{}, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return nil, err
	}
	if decoded == nil {
		return map[string]any{}, nil
	}
	return decoded, nil
}

func parseJSONValue(value string) (any, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTarget(row rowScanner) (Target, error) {
	var configJSON string
	var target Target
	if err := row.Scan(
		&target.ID,
		&target.ConnectorKind,
		&target.Name,
		&configJSON,
		&target.Status,
		&target.CreatedAt,
		&target.UpdatedAt,
	); err != nil {
		return Target{}, err
	}
	config, err := parseJSONObject(configJSON)
	if err != nil {
		return Target{}, fmt.Errorf("decode connector target config: %w", err)
	}
	target.Config = config
	return target, nil
}

func scanCredentialProfile(row rowScanner) (CredentialProfile, error) {
	var publicJSON string
	var profile CredentialProfile
	if err := row.Scan(
		&profile.ID,
		&profile.TargetID,
		&profile.ConnectorKind,
		&profile.Kind,
		&profile.Label,
		&publicJSON,
		&profile.EncryptedSecretJSON,
		&profile.RiskLabel,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	); err != nil {
		return CredentialProfile{}, err
	}
	public, err := parseJSONObject(publicJSON)
	if err != nil {
		return CredentialProfile{}, fmt.Errorf("decode connector profile public metadata: %w", err)
	}
	profile.Public = public
	return profile, nil
}

func scanActionPermission(row rowScanner) (ActionPermission, error) {
	var item ActionPermission
	if err := row.Scan(
		&item.TokenID,
		&item.TargetID,
		&item.TargetName,
		&item.ProfileID,
		&item.ProfileLabel,
		&item.ConnectorKind,
		&item.ProfileKind,
		&item.ActionName,
		&item.ExecutionRule,
		&item.ExpiresAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return ActionPermission{}, fmt.Errorf("scan connector action permission: %w", err)
	}
	return item, nil
}

func actionRequestSelectSQL() string {
	return `
		SELECT
			r.id, r.token_id, COALESCE(tok.name, ''),
			r.target_id, t.name, r.profile_id, p.label,
			r.connector_kind, r.action_name, r.source, r.input_json, r.encrypted_payload_json,
			r.reason, r.status, r.output_json, r.display_text, r.error,
			r.approval_context, r.approval_context_hash, r.approval_context_drift,
			r.created_at, r.completed_at
		FROM connector_action_requests r
		JOIN connector_targets t ON t.id = r.target_id
		JOIN connector_credential_profiles p ON p.id = r.profile_id AND p.target_id = r.target_id AND p.connector_kind = r.connector_kind
		LEFT JOIN api_tokens tok ON tok.id = r.token_id`
}

func scanActionRequest(row rowScanner) (ActionRequest, error) {
	var request ActionRequest
	var tokenID sql.NullInt64
	var inputJSON string
	var outputJSON string
	var completedAt sql.NullString
	if err := row.Scan(
		&request.ID,
		&tokenID,
		&request.TokenName,
		&request.TargetID,
		&request.TargetName,
		&request.ProfileID,
		&request.ProfileLabel,
		&request.ConnectorKind,
		&request.ActionName,
		&request.Source,
		&inputJSON,
		&request.EncryptedPayloadJSON,
		&request.Reason,
		&request.Status,
		&outputJSON,
		&request.DisplayText,
		&request.Error,
		&request.ApprovalContext,
		&request.ApprovalContextHash,
		&request.ApprovalContextDrift,
		&request.CreatedAt,
		&completedAt,
	); err != nil {
		return ActionRequest{}, fmt.Errorf("scan connector action request: %w", err)
	}
	if tokenID.Valid {
		request.TokenID = &tokenID.Int64
	}
	input, err := parseJSONObject(inputJSON)
	if err != nil {
		return ActionRequest{}, fmt.Errorf("decode connector action input: %w", err)
	}
	request.Input = input
	output, err := parseJSONValue(outputJSON)
	if err != nil {
		return ActionRequest{}, fmt.Errorf("decode connector action output: %w", err)
	}
	request.Output = output
	if completedAt.Valid {
		request.CompletedAt = &completedAt.String
	}
	return request, nil
}

func validateActionRequestInput(input InsertActionRequestInput) error {
	if input.TargetID < 1 || input.ProfileID < 1 {
		return ValidationError("target_id and profile_id are required")
	}
	if input.TokenID != nil && *input.TokenID < 1 {
		return ValidationError("token_id must be positive")
	}
	if !connectors.ValidIdentifier(input.ConnectorKind) {
		return ValidationError("invalid connector kind")
	}
	if !connectors.ValidIdentifier(input.ActionName) {
		return ValidationError("invalid action name")
	}
	if !validActionRequestStatus(input.Status) {
		return ValidationError("invalid action request status")
	}
	return nil
}

func actionRequestSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "mcp"
	}
	return source
}

func validActionRequestStatus(status connectors.ResultStatus) bool {
	switch status {
	case connectors.ResultCompleted,
		connectors.ResultFailed,
		connectors.ResultCanceled,
		connectors.ResultRunning,
		connectors.ResultApprovalPending,
		connectors.ResultBlocked,
		connectors.ResultStale,
		connectors.ResultDeclined,
		connectors.ResultError:
		return true
	default:
		return false
	}
}

func validActionRequestTerminalStatus(status connectors.ResultStatus) bool {
	switch status {
	case connectors.ResultCompleted,
		connectors.ResultFailed,
		connectors.ResultCanceled,
		connectors.ResultBlocked,
		connectors.ResultStale,
		connectors.ResultDeclined,
		connectors.ResultError:
		return true
	default:
		return false
	}
}

func finishAllowedStatuses(statuses []connectors.ResultStatus) ([]connectors.ResultStatus, error) {
	if len(statuses) == 0 {
		return []connectors.ResultStatus{connectors.ResultRunning}, nil
	}
	allowed := make([]connectors.ResultStatus, 0, len(statuses))
	seen := map[connectors.ResultStatus]bool{}
	for _, status := range statuses {
		switch status {
		case connectors.ResultRunning, connectors.ResultApprovalPending, connectors.ResultBlocked:
			if !seen[status] {
				allowed = append(allowed, status)
				seen[status] = true
			}
		default:
			return nil, ValidationError("invalid allowed action request status")
		}
	}
	if len(allowed) == 0 {
		return nil, ValidationError("allowed action request statuses are required")
	}
	return allowed, nil
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "constraint failed")
}
