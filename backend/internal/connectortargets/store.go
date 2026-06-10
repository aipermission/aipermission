package connectortargets

import (
	"context"
	"database/sql"
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

type CreateTargetInput struct {
	ConnectorKind string
	Name          string
	Config        map[string]any
}

func (s *Store) CreateTarget(ctx context.Context, input CreateTargetInput) (Target, error) {
	if s == nil || s.db == nil {
		return Target{}, fmt.Errorf("connector target store is not configured")
	}
	if !connectors.ValidIdentifier(input.ConnectorKind) {
		return Target{}, fmt.Errorf("invalid connector kind %q", input.ConnectorKind)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Target{}, fmt.Errorf("target name is required")
	}
	configJSON, err := jsonObjectString(input.Config)
	if err != nil {
		return Target{}, fmt.Errorf("encode target config: %w", err)
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

type CreateCredentialProfileInput struct {
	TargetID            int64
	ConnectorKind       string
	Kind                string
	Label               string
	Public              map[string]any
	EncryptedSecretJSON string
	RiskLabel           string
}

func (s *Store) CreateCredentialProfile(ctx context.Context, input CreateCredentialProfileInput) (CredentialProfile, error) {
	if s == nil || s.db == nil {
		return CredentialProfile{}, fmt.Errorf("connector target store is not configured")
	}
	if input.TargetID < 1 {
		return CredentialProfile{}, fmt.Errorf("target_id is required")
	}
	if !connectors.ValidIdentifier(input.ConnectorKind) {
		return CredentialProfile{}, fmt.Errorf("invalid connector kind %q", input.ConnectorKind)
	}
	if !connectors.ValidIdentifier(input.Kind) {
		return CredentialProfile{}, fmt.Errorf("invalid credential kind %q", input.Kind)
	}
	label := strings.TrimSpace(input.Label)
	if label == "" {
		return CredentialProfile{}, fmt.Errorf("profile label is required")
	}
	publicJSON, err := jsonObjectString(input.Public)
	if err != nil {
		return CredentialProfile{}, fmt.Errorf("encode profile public metadata: %w", err)
	}
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO connector_credential_profiles (
			target_id, connector_kind, kind, label, public_json, encrypted_secret_json,
			risk_label, created_at, updated_at
		)
		SELECT id, ?, ?, ?, ?, ?, ?, ?, ?
		FROM connector_targets
		WHERE id = ? AND connector_kind = ? AND status = 'active'`,
		input.ConnectorKind,
		input.Kind,
		label,
		publicJSON,
		input.EncryptedSecretJSON,
		strings.TrimSpace(input.RiskLabel),
		now,
		now,
		input.TargetID,
		input.ConnectorKind,
	)
	if err != nil {
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

type SetActionPermissionInput struct {
	TokenID       int64
	TargetID      int64
	ProfileID     int64
	ActionName    string
	ExecutionRule ActionPermissionRule
	ExpiresAt     *time.Time
}

func (s *Store) SetActionPermission(ctx context.Context, input SetActionPermissionInput) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("connector target store is not configured")
	}
	if input.TokenID < 1 || input.TargetID < 1 || input.ProfileID < 1 {
		return fmt.Errorf("token_id, target_id, and profile_id are required")
	}
	if !connectors.ValidIdentifier(input.ActionName) {
		return fmt.Errorf("invalid action name %q", input.ActionName)
	}
	switch input.ExecutionRule {
	case ActionPermissionAlwaysRun, ActionPermissionApprovalRequired, ActionPermissionBlocked:
	default:
		return fmt.Errorf("invalid execution rule %q", input.ExecutionRule)
	}
	var expiresAt any
	if input.ExpiresAt != nil {
		expiresAt = input.ExpiresAt.UTC().Format(time.RFC3339Nano)
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
		expiresAt,
		now,
		now,
	)
	return err
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
	var profilePublicJSON string
	var target connectors.TargetView
	var profile connectors.CredentialProfileView
	err := s.db.QueryRowContext(ctx, `
		SELECT
			t.id, t.connector_kind, t.name, t.config_json,
			p.id, p.target_id, p.connector_kind, p.kind, p.label, p.public_json
		FROM connector_targets t
		JOIN connector_credential_profiles p ON p.target_id = t.id
		WHERE
			t.id = ?
			AND p.id = ?
			AND t.connector_kind = ?
			AND p.connector_kind = t.connector_kind
			AND t.status = 'active'`,
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
	return target, profile, nil
}

var (
	ErrInvalidTargetRef      = errors.New("invalid connector target ref")
	ErrTargetProfileNotFound = errors.New("connector target profile not found")
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
