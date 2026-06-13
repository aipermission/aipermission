package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
)

type Resolver struct {
	db *sql.DB
}

func NewResolver(db *sql.DB) *Resolver {
	return &Resolver{db: db}
}

type SSHRuntimeMapping struct {
	TargetID  int64
	ProfileID int64
	ServerID  int64
	SSHKeyID  int64
	Username  string
}

func SSHTargetRef(targetID int64, profileID int64) string {
	return ConnectorTargetRef(sshconnector.Kind, targetID, profileID)
}

func ParseSSHTargetRef(ref string) (int64, int64, bool) {
	kind, targetID, profileID, ok := ParseConnectorTargetRef(ref)
	if !ok || kind != sshconnector.Kind {
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

func (s *Store) SSHRuntimeForConsoleID(ctx context.Context, profileID int64) (connectors.TargetView, connectors.CredentialProfileView, error) {
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
			AND p.connector_kind = ?
			AND t.connector_kind = ?
			AND p.connector_kind = t.connector_kind
			AND p.status = 'active'
			AND t.status = 'active'`,
		profileID,
		sshconnector.Kind,
		sshconnector.Kind,
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

func (s *Store) SSHRuntimeMappingForConsoleID(ctx context.Context, profileID int64) (SSHRuntimeMapping, error) {
	target, profile, err := s.SSHRuntimeForConsoleID(ctx, profileID)
	if err == nil {
		return SSHRuntimeMapping{
			TargetID:  target.ID,
			ProfileID: profile.ID,
			ServerID:  profile.ID,
			SSHKeyID:  int64MapValue(profile.Public, "ssh_key_id"),
			Username:  stringMapValue(profile.Public, "username"),
		}, nil
	}
	return SSHRuntimeMapping{}, err
}

func (s *Store) SSHRuntimeForConsoleRef(ctx context.Context, targetRef string) (connectors.TargetView, connectors.CredentialProfileView, error) {
	target, profile, err := s.ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, err
	}
	if target.ConnectorKind != sshconnector.Kind || profile.ConnectorKind != sshconnector.Kind {
		return connectors.TargetView{}, connectors.CredentialProfileView{}, ErrInvalidTargetRef
	}
	return target, profile, nil
}

func (s *Store) SSHRuntimeForTargetRef(ctx context.Context, targetRef string) (SSHRuntimeMapping, connectors.TargetView, connectors.CredentialProfileView, error) {
	_, _, ok := ParseSSHTargetRef(targetRef)
	if !ok {
		return SSHRuntimeMapping{}, connectors.TargetView{}, connectors.CredentialProfileView{}, ErrInvalidTargetRef
	}
	target, profile, err := s.ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return SSHRuntimeMapping{}, connectors.TargetView{}, connectors.CredentialProfileView{}, err
	}
	mapping := SSHRuntimeMapping{
		TargetID:  target.ID,
		ProfileID: profile.ID,
		ServerID:  profile.ID,
		SSHKeyID:  int64MapValue(profile.Public, "ssh_key_id"),
		Username:  stringMapValue(profile.Public, "username"),
	}
	return mapping, target, profile, nil
}

func stringMapValue(value map[string]any, key string) string {
	raw, ok := value[key]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func int64MapValue(value map[string]any, key string) int64 {
	raw, ok := value[key]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		var parsed int64
		if _, err := fmt.Sscan(v, &parsed); err == nil {
			return parsed
		}
	}
	return 0
}
