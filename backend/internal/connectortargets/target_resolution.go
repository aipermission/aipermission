package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const connectorTargetRefSeparator = ":"

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
			t.id, t.project_id, t.connector_kind, t.name, t.config_json, t.updated_at,
			p.id, p.target_id, p.connector_kind, p.kind, p.label, p.public_json,
			p.encrypted_secret_json, p.risk_label, p.secret_revision, p.updated_at
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
