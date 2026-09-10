package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

// ValidateTransportProject ensures a connector can only route through a
// transport target in the same project. This keeps project boundaries in the
// shared transport pipeline instead of duplicating policy in each connector.
func (s *Store) ValidateTransportProject(ctx context.Context, projectID int64, transportTargetRef string) error {
	resolvedProjectID, err := s.resolveProjectID(ctx, projectID)
	if err != nil {
		return err
	}
	transportTarget, _, err := s.ResolveConnectorActionTarget(ctx, transportTargetRef)
	if err != nil {
		return err
	}
	if transportTarget.ProjectID != resolvedProjectID {
		return ValidationError("transport target must belong to the same project")
	}
	return nil
}

func (s *Store) ValidateTransportTarget(ctx context.Context, sourceTargetRef string, transportTargetRef string) error {
	sourceTarget, _, err := s.ResolveConnectorActionTarget(ctx, sourceTargetRef)
	if err != nil {
		return err
	}
	return s.ValidateTransportProject(ctx, sourceTarget.ProjectID, transportTargetRef)
}

func (s *Store) ResolveConnectorActionTarget(ctx context.Context, targetRef string) (connectors.TargetView, connectors.CredentialProfileView, error) {
	if s == nil || s.db == nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, fmt.Errorf("connector target store is not configured")
	}
	connectorKind, targetID, profileID, ok := connectors.ParseTargetRef(targetRef)
	if !ok {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, ErrInvalidTargetRef
	}
	return s.resolveTargetProfileViews(ctx, targetID, profileID, connectorKind)
}

func (s *Store) ResolveTargetProfileViews(ctx context.Context, targetID, profileID int64) (connectors.TargetView, connectors.CredentialProfileView, error) {
	if s == nil || s.db == nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, fmt.Errorf("connector target store is not configured")
	}
	return s.resolveTargetProfileViews(ctx, targetID, profileID, "")
}

func (s *Store) resolveTargetProfileViews(ctx context.Context, targetID, profileID int64, connectorKind string) (connectors.TargetView, connectors.CredentialProfileView, error) {
	target, profile, err := s.resolveTargetProfile(ctx, targetID, profileID, connectorKind)
	return target, CredentialProfileView(profile), err
}

func (s *Store) resolveTargetProfile(ctx context.Context, targetID, profileID int64, connectorKind string) (connectors.TargetView, CredentialProfile, error) {
	var targetConfigJSON string
	var target connectors.TargetView
	var profile CredentialProfile
	var profilePublicJSON string
	query := `
		SELECT
			t.id, t.project_id, t.connector_kind, t.name, t.config_json, t.updated_at,
			p.id, p.target_id, p.connector_kind, p.kind, p.label, p.public_json,
			p.encrypted_secret_json, p.risk_label, p.secret_revision, p.created_at, p.updated_at
		FROM connector_targets t
		JOIN connector_credential_profiles p ON p.target_id = t.id
		WHERE
			t.id = ?
			AND p.id = ?
			AND p.connector_kind = t.connector_kind
			AND t.status = 'active'
			AND p.status = 'active'`
	args := []any{targetID, profileID}
	if connectorKind != "" {
		query += ` AND t.connector_kind = ?`
		args = append(args, connectorKind)
	}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&target.ID,
		&target.ProjectID,
		&target.ConnectorKind,
		&target.Name,
		&targetConfigJSON,
		&target.UpdatedAt,
		&profile.ID,
		&profile.TargetID,
		&profile.ConnectorKind,
		&profile.Kind,
		&profile.Label,
		&profilePublicJSON,
		&profile.EncryptedSecretJSON,
		&profile.RiskLabel,
		&profile.SecretRevision,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return connectors.TargetView{}, CredentialProfile{}, ErrTargetProfileNotFound
	}
	if err != nil {
		return connectors.TargetView{}, CredentialProfile{}, err
	}
	target.Ref = connectors.FormatTargetRef(target.ConnectorKind, target.ID, profile.ID)
	target.Config, err = parseJSONObject(targetConfigJSON)
	if err != nil {
		return connectors.TargetView{}, CredentialProfile{}, fmt.Errorf("decode target config: %w", err)
	}
	profile.Public, err = parseJSONObject(profilePublicJSON)
	if err != nil {
		return connectors.TargetView{}, CredentialProfile{}, fmt.Errorf("decode profile public metadata: %w", err)
	}
	return target, profile, nil
}
