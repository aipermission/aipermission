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

func (s *Store) TargetProfileByProfileID(ctx context.Context, profileID int64) (connectors.TargetView, connectors.CredentialProfileView, error) {
	if s == nil || s.db == nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, fmt.Errorf("connector target store is not configured")
	}
	if profileID < 1 {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, ErrTargetProfileNotFound
	}
	var target connectors.TargetView
	var profile CredentialProfile
	var targetConfigJSON string
	var profilePublicJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT
			t.id, t.connector_kind, t.name, t.config_json,
			p.id, p.target_id, p.connector_kind, p.kind, p.label, p.public_json,
			p.encrypted_secret_json, p.risk_label, p.updated_at
		FROM connector_credential_profiles p
		JOIN connector_targets t ON t.id = p.target_id
		WHERE p.id = ?
			AND p.connector_kind = t.connector_kind
			AND p.status = 'active'
			AND t.status = 'active'`,
		profileID,
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
		return connectors.TargetView{}, connectors.CredentialProfileView{}, err
	}
	profile.Public, err = parseJSONObject(profilePublicJSON)
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, err
	}
	return target, CredentialProfileView(profile), nil
}
