package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
)

const sshTargetPrefix = "ssh:"

type Resolver struct {
	db *sql.DB
}

func NewResolver(db *sql.DB) *Resolver {
	return &Resolver{db: db}
}

func SSHTargetRef(serverID int64) string {
	return fmt.Sprintf("%s%d", sshTargetPrefix, serverID)
}

func ParseSSHTargetRef(ref string) (int64, bool) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, sshTargetPrefix) {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(ref, sshTargetPrefix), 10, 64)
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}

func (r *Resolver) ResolveActionTarget(ctx context.Context, targetRef string) (actions.ResolvedTarget, error) {
	serverID, ok := ParseSSHTargetRef(targetRef)
	if !ok {
		return actions.ResolvedTarget{}, actions.ErrTargetNotFound
	}

	var row legacySSHServerRow
	err := r.db.QueryRowContext(ctx, `
		SELECT
			s.id, s.name, s.host, s.port, s.username, s.ssh_key_id,
			COALESCE(k.name, ''), COALESCE(k.key_type, ''), COALESCE(k.fingerprint, ''), COALESCE(k.public_key, ''),
			s.description, s.startup_input_after_connect, s.force_shell_command
		FROM servers s
		LEFT JOIN ssh_keys k ON k.id = s.ssh_key_id
		WHERE s.id = ?`,
		serverID,
	).Scan(
		&row.ID,
		&row.Name,
		&row.Host,
		&row.Port,
		&row.Username,
		&row.SSHKeyID,
		&row.SSHKeyName,
		&row.SSHKeyType,
		&row.SSHKeyFingerprint,
		&row.SSHPublicKey,
		&row.Description,
		&row.StartupInputAfterConnect,
		&row.ForceShellCommand,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return actions.ResolvedTarget{}, actions.ErrTargetNotFound
	}
	if err != nil {
		return actions.ResolvedTarget{}, err
	}
	if row.SSHKeyID < 1 || row.SSHKeyName == "" {
		return actions.ResolvedTarget{}, actions.ErrTargetNotFound
	}

	target := connectors.TargetView{
		ID:            row.ID,
		Ref:           SSHTargetRef(row.ID),
		ConnectorKind: sshconnector.Kind,
		Name:          row.Name,
		Config: map[string]any{
			"host":                        row.Host,
			"port":                        row.Port,
			"description":                 row.Description,
			"startup_input_after_connect": row.StartupInputAfterConnect,
			"force_shell_command":         row.ForceShellCommand,
		},
	}
	profile := connectors.CredentialProfileView{
		ID:            row.SSHKeyID,
		TargetID:      row.ID,
		ConnectorKind: sshconnector.Kind,
		Kind:          "private_key",
		Label:         row.SSHKeyName,
		Public: map[string]any{
			"username":    row.Username,
			"ssh_key_id":  row.SSHKeyID,
			"key_name":    row.SSHKeyName,
			"key_type":    row.SSHKeyType,
			"fingerprint": row.SSHKeyFingerprint,
			"public_key":  row.SSHPublicKey,
		},
	}
	return actions.ResolvedTarget{Target: target, Profile: profile}, nil
}

type legacySSHServerRow struct {
	ID                       int64
	Name                     string
	Host                     string
	Port                     int
	Username                 string
	SSHKeyID                 int64
	SSHKeyName               string
	SSHKeyType               string
	SSHKeyFingerprint        string
	SSHPublicKey             string
	Description              string
	StartupInputAfterConnect string
	ForceShellCommand        string
}
