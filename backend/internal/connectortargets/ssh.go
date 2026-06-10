package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

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

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type sshServerSyncInput struct {
	ServerID                 int64
	Name                     string
	Host                     string
	Port                     int
	Username                 string
	SSHKeyID                 int64
	SSHKeyName               string
	SSHKeyType               string
	SSHKeyFingerprint        string
	Description              string
	StartupInputAfterConnect string
	ForceShellCommand        string
	CreatedAt                string
	UpdatedAt                string
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

func (s *Store) SSHTargetRefForServer(ctx context.Context, serverID int64) (string, error) {
	mapping, err := s.SSHRuntimeForServer(ctx, serverID)
	if err != nil {
		return "", err
	}
	return SSHTargetRef(mapping.TargetID, mapping.ProfileID), nil
}

func (s *Store) SSHRuntimeForServer(ctx context.Context, serverID int64) (SSHRuntimeMapping, error) {
	if s == nil || s.db == nil {
		return SSHRuntimeMapping{}, fmt.Errorf("connector target store is not configured")
	}
	var mapping SSHRuntimeMapping
	var profilePublicJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT r.target_id, r.profile_id, r.server_id, p.public_json
		FROM ssh_connector_profile_runtimes r
		JOIN connector_targets t ON t.id = r.target_id
		JOIN connector_credential_profiles p ON p.id = r.profile_id AND p.target_id = r.target_id
		WHERE r.server_id = ? AND t.connector_kind = ? AND t.status = 'active'`,
		serverID,
		sshconnector.Kind,
	).Scan(&mapping.TargetID, &mapping.ProfileID, &mapping.ServerID, &profilePublicJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return SSHRuntimeMapping{}, ErrTargetProfileNotFound
	}
	if err != nil {
		return SSHRuntimeMapping{}, err
	}
	applySSHPublicMetadata(&mapping, profilePublicJSON)
	return mapping, nil
}

func (s *Store) SSHRuntimeForTargetRef(ctx context.Context, targetRef string) (SSHRuntimeMapping, connectors.TargetView, connectors.CredentialProfileView, error) {
	targetID, profileID, ok := ParseSSHTargetRef(targetRef)
	if !ok {
		return SSHRuntimeMapping{}, connectors.TargetView{}, connectors.CredentialProfileView{}, ErrInvalidTargetRef
	}
	target, profile, err := s.ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return SSHRuntimeMapping{}, connectors.TargetView{}, connectors.CredentialProfileView{}, err
	}
	var mapping SSHRuntimeMapping
	var profilePublicJSON string
	err = s.db.QueryRowContext(ctx, `
		SELECT r.target_id, r.profile_id, r.server_id, p.public_json
		FROM ssh_connector_profile_runtimes r
		JOIN connector_credential_profiles p ON p.id = r.profile_id AND p.target_id = r.target_id
		WHERE r.target_id = ? AND r.profile_id = ?`,
		targetID,
		profileID,
	).Scan(&mapping.TargetID, &mapping.ProfileID, &mapping.ServerID, &profilePublicJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return SSHRuntimeMapping{}, connectors.TargetView{}, connectors.CredentialProfileView{}, ErrTargetProfileNotFound
	}
	if err != nil {
		return SSHRuntimeMapping{}, connectors.TargetView{}, connectors.CredentialProfileView{}, err
	}
	applySSHPublicMetadata(&mapping, profilePublicJSON)
	return mapping, target, profile, nil
}

func SyncSSHServerByID(ctx context.Context, tx *sql.Tx, serverID int64) (string, error) {
	if tx == nil {
		return "", fmt.Errorf("transaction is required")
	}
	input, err := readSSHServerSyncInput(ctx, tx, serverID)
	if err != nil {
		return "", err
	}
	return syncSSHServer(ctx, tx, input)
}

func DeleteSSHServerMapping(ctx context.Context, tx *sql.Tx, serverID int64) error {
	if tx == nil {
		return fmt.Errorf("transaction is required")
	}
	var targetID int64
	err := tx.QueryRowContext(ctx, `SELECT target_id FROM ssh_connector_profile_runtimes WHERE server_id = ?`, serverID).Scan(&targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM connector_targets WHERE id = ? AND connector_kind = ?`, targetID, sshconnector.Kind); err != nil {
		return err
	}
	return nil
}

func (s *Store) SyncSSHServers(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("connector target store is not configured")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id
		FROM servers s
		ORDER BY s.id`)
	if err != nil {
		return fmt.Errorf("list ssh servers for connector sync: %w", err)
	}
	defer rows.Close()

	serverIDs := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan ssh server for connector sync: %w", err)
		}
		serverIDs = append(serverIDs, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate ssh servers for connector sync: %w", err)
	}
	for _, serverID := range serverIDs {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin ssh connector sync: %w", err)
		}
		if _, err := SyncSSHServerByID(ctx, tx, serverID); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sync ssh server %d as connector target: %w", serverID, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit ssh connector sync: %w", err)
		}
	}
	return nil
}

func readSSHServerSyncInput(ctx context.Context, runner sqlExecutor, serverID int64) (sshServerSyncInput, error) {
	var input sshServerSyncInput
	err := runner.QueryRowContext(ctx, `
		SELECT
			s.id, s.name, s.host, s.port, s.username, s.ssh_key_id,
			COALESCE(k.name, ''), COALESCE(k.key_type, ''), COALESCE(k.fingerprint, ''),
			s.description, s.startup_input_after_connect, s.force_shell_command,
			s.created_at, s.updated_at
		FROM servers s
		LEFT JOIN ssh_keys k ON k.id = s.ssh_key_id
		WHERE s.id = ?`,
		serverID,
	).Scan(
		&input.ServerID,
		&input.Name,
		&input.Host,
		&input.Port,
		&input.Username,
		&input.SSHKeyID,
		&input.SSHKeyName,
		&input.SSHKeyType,
		&input.SSHKeyFingerprint,
		&input.Description,
		&input.StartupInputAfterConnect,
		&input.ForceShellCommand,
		&input.CreatedAt,
		&input.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return sshServerSyncInput{}, ErrTargetNotFound
	}
	if err != nil {
		return sshServerSyncInput{}, err
	}
	if input.SSHKeyID < 1 || strings.TrimSpace(input.Username) == "" {
		return sshServerSyncInput{}, ValidationError("ssh server must have a username and ssh key")
	}
	return input, nil
}

func syncSSHServer(ctx context.Context, runner sqlExecutor, input sshServerSyncInput) (string, error) {
	targetConfig, err := jsonObjectString(map[string]any{
		"host":                        input.Host,
		"port":                        input.Port,
		"description":                 input.Description,
		"startup_input_after_connect": input.StartupInputAfterConnect,
		"force_shell_command":         input.ForceShellCommand,
	})
	if err != nil {
		return "", err
	}
	profilePublic, err := jsonObjectString(map[string]any{
		"username":    input.Username,
		"ssh_key_id":  input.SSHKeyID,
		"key_name":    input.SSHKeyName,
		"key_type":    input.SSHKeyType,
		"fingerprint": input.SSHKeyFingerprint,
	})
	if err != nil {
		return "", err
	}

	targetID, profileID, err := existingSSHRuntimeIDs(ctx, runner, input.ServerID)
	if err != nil {
		return "", err
	}
	if targetID == 0 {
		targetID, err = existingTargetIDByKindName(ctx, runner, sshconnector.Kind, input.Name)
		if err != nil {
			return "", err
		}
	}

	now := strings.TrimSpace(input.UpdatedAt)
	if now == "" {
		now = nowString()
	}
	if targetID == 0 {
		result, err := runner.ExecContext(ctx, `
			INSERT INTO connector_targets (connector_kind, name, config_json, status, created_at, updated_at)
			VALUES (?, ?, ?, 'active', ?, ?)`,
			sshconnector.Kind,
			input.Name,
			targetConfig,
			valueOrNow(input.CreatedAt),
			now,
		)
		if err != nil {
			return "", err
		}
		targetID, err = result.LastInsertId()
		if err != nil {
			return "", err
		}
	} else if _, err := runner.ExecContext(ctx, `
		UPDATE connector_targets
		SET connector_kind = ?, name = ?, config_json = ?, status = 'active', updated_at = ?
		WHERE id = ?`,
		sshconnector.Kind,
		input.Name,
		targetConfig,
		now,
		targetID,
	); err != nil {
		return "", err
	}

	profileLabel := sshProfileLabel(input)
	if profileID == 0 {
		result, err := runner.ExecContext(ctx, `
			INSERT INTO connector_credential_profiles (
				target_id, connector_kind, kind, label, public_json, encrypted_secret_json,
				risk_label, created_at, updated_at
			)
			VALUES (?, ?, 'private_key', ?, ?, '', '', ?, ?)`,
			targetID,
			sshconnector.Kind,
			profileLabel,
			profilePublic,
			valueOrNow(input.CreatedAt),
			now,
		)
		if err != nil {
			return "", err
		}
		profileID, err = result.LastInsertId()
		if err != nil {
			return "", err
		}
	} else if _, err := runner.ExecContext(ctx, `
		UPDATE connector_credential_profiles
		SET connector_kind = ?, kind = 'private_key', label = ?, public_json = ?, updated_at = ?
		WHERE id = ? AND target_id = ?`,
		sshconnector.Kind,
		profileLabel,
		profilePublic,
		now,
		profileID,
		targetID,
	); err != nil {
		return "", err
	}

	if _, err := runner.ExecContext(ctx, `
		INSERT INTO ssh_connector_profile_runtimes (target_id, profile_id, server_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(server_id) DO UPDATE SET
			target_id = excluded.target_id,
			profile_id = excluded.profile_id,
			updated_at = excluded.updated_at`,
		targetID,
		profileID,
		input.ServerID,
		valueOrNow(input.CreatedAt),
		now,
	); err != nil {
		return "", err
	}
	return SSHTargetRef(targetID, profileID), nil
}

func existingSSHRuntimeIDs(ctx context.Context, runner sqlExecutor, serverID int64) (int64, int64, error) {
	var targetID int64
	var profileID int64
	err := runner.QueryRowContext(ctx, `
		SELECT target_id, profile_id
		FROM ssh_connector_profile_runtimes
		WHERE server_id = ?`,
		serverID,
	).Scan(&targetID, &profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	return targetID, profileID, nil
}

func existingTargetIDByKindName(ctx context.Context, runner sqlExecutor, connectorKind string, name string) (int64, error) {
	var targetID int64
	err := runner.QueryRowContext(ctx, `
		SELECT id
		FROM connector_targets
		WHERE connector_kind = ? AND name = ?`,
		connectorKind,
		name,
	).Scan(&targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return targetID, nil
}

func sshProfileLabel(input sshServerSyncInput) string {
	username := strings.TrimSpace(input.Username)
	if username != "" {
		return username
	}
	keyName := strings.TrimSpace(input.SSHKeyName)
	if keyName != "" {
		return keyName
	}
	return "default"
}

func valueOrNow(value string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return nowString()
}

func applySSHPublicMetadata(mapping *SSHRuntimeMapping, profilePublicJSON string) {
	public, err := parseJSONObject(profilePublicJSON)
	if err != nil {
		return
	}
	mapping.Username = stringMapValue(public, "username")
	mapping.SSHKeyID = int64MapValue(public, "ssh_key_id")
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
