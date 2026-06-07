package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	approvalContextSchemaVersion = 1
	approvalContextToolName      = "mcp.exec"
)

type approvalContextSnapshot struct {
	SchemaVersion int                       `json:"schema_version"`
	CapturedAt    string                    `json:"captured_at"`
	Tool          approvalContextTool       `json:"tool"`
	Token         approvalContextToken      `json:"token"`
	Permission    approvalContextPermission `json:"permission"`
	Server        approvalContextServer     `json:"server"`
	Command       approvalContextCommand    `json:"command"`
}

type approvalContextTool struct {
	Name          string `json:"name"`
	SchemaVersion int    `json:"schema_version"`
}

type approvalContextToken struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ExpiresAt string `json:"expires_at,omitempty"`
	RevokedAt string `json:"revoked_at,omitempty"`
}

type approvalContextPermission struct {
	Rule      string `json:"rule"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type approvalContextServer struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	Username          string `json:"username"`
	Description       string `json:"description,omitempty"`
	AuthType          string `json:"auth_type"`
	KeyLabel          string `json:"key_label,omitempty"`
	SSHKeyID          int64  `json:"ssh_key_id"`
	SSHKeyFingerprint string `json:"ssh_key_fingerprint,omitempty"`
}

type approvalContextCommand struct {
	SHA256 string `json:"sha256"`
}

func (s *Server) buildApprovalContextSnapshot(ctx context.Context, runtime *databaseRuntime, tokenID int64, serverID int64, command string, capturedAt string) (string, string, error) {
	snapshot, err := s.readCurrentApprovalContext(ctx, runtime, tokenID, serverID, command)
	if err != nil {
		return "", "", err
	}
	snapshot.CapturedAt = capturedAt
	hash, err := hashApprovalContext(snapshot)
	if err != nil {
		return "", "", err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return "", "", fmt.Errorf("marshal approval context: %w", err)
	}
	return string(payload), hash, nil
}

func (s *Server) readCurrentApprovalContext(ctx context.Context, runtime *databaseRuntime, tokenID int64, serverID int64, command string) (approvalContextSnapshot, error) {
	var snapshot approvalContextSnapshot
	snapshot.SchemaVersion = approvalContextSchemaVersion
	snapshot.Tool = approvalContextTool{Name: approvalContextToolName, SchemaVersion: approvalContextSchemaVersion}
	snapshot.Command = approvalContextCommand{SHA256: sha256Hex(command)}

	err := runtime.database.QueryRowContext(ctx, `
		SELECT tok.id, tok.name, COALESCE(tok.expires_at, ''), COALESCE(tok.revoked_at, ''),
		       p.execution_rule, COALESCE(p.expires_at, ''),
		       srv.id, srv.name, srv.host, srv.port, srv.username, srv.description, srv.auth_type, srv.key_label, srv.ssh_key_id,
		       COALESCE(k.fingerprint, '')
		FROM api_tokens tok
		JOIN token_server_permissions p ON p.token_id = tok.id AND p.server_id = ?
		JOIN servers srv ON srv.id = p.server_id
		LEFT JOIN ssh_keys k ON k.id = srv.ssh_key_id
		WHERE tok.id = ?`,
		serverID,
		tokenID,
	).Scan(
		&snapshot.Token.ID,
		&snapshot.Token.Name,
		&snapshot.Token.ExpiresAt,
		&snapshot.Token.RevokedAt,
		&snapshot.Permission.Rule,
		&snapshot.Permission.ExpiresAt,
		&snapshot.Server.ID,
		&snapshot.Server.Name,
		&snapshot.Server.Host,
		&snapshot.Server.Port,
		&snapshot.Server.Username,
		&snapshot.Server.Description,
		&snapshot.Server.AuthType,
		&snapshot.Server.KeyLabel,
		&snapshot.Server.SSHKeyID,
		&snapshot.Server.SSHKeyFingerprint,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return approvalContextSnapshot{}, err
	}
	if err != nil {
		return approvalContextSnapshot{}, fmt.Errorf("read approval context: %w", err)
	}
	return snapshot, nil
}

func (s *Server) approvalContextDrift(ctx context.Context, runtime *databaseRuntime, request commandRequestRecord, command string) (bool, string, error) {
	if request.TokenID == nil {
		return true, "approval context is missing its token identity", nil
	}
	var storedPayload string
	var storedHash string
	err := runtime.database.QueryRowContext(ctx, `
		SELECT approval_context, approval_context_hash
		FROM command_requests
		WHERE id = ?`,
		request.ID,
	).Scan(&storedPayload, &storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return true, "approval request no longer exists", nil
	}
	if err != nil {
		return false, "", err
	}
	if storedPayload == "" || storedHash == "" {
		return false, "", nil
	}

	var stored approvalContextSnapshot
	if err := json.Unmarshal([]byte(storedPayload), &stored); err != nil {
		return true, "stored approval context is unreadable", nil
	}
	if reason := currentApprovalValidityProblem(stored, time.Now().UTC()); reason != "" {
		return true, reason, nil
	}

	current, err := s.readCurrentApprovalContext(ctx, runtime, *request.TokenID, request.ServerID, command)
	if errors.Is(err, sql.ErrNoRows) {
		return true, "token permission, token, or server no longer exists", nil
	}
	if err != nil {
		return false, "", err
	}
	current.CapturedAt = stored.CapturedAt
	currentHash, err := hashApprovalContext(current)
	if err != nil {
		return false, "", err
	}
	if currentHash == storedHash {
		return false, "", nil
	}
	return true, describeApprovalContextDrift(stored, current), nil
}

func (s *Server) markCommandRequestStale(ctx context.Context, runtime *databaseRuntime, id int64, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := runtime.database.ExecContext(ctx, `
		UPDATE command_requests
		SET status = 'stale', approval_context_drift = ?, error = ?, completed_at = ?
		WHERE id = ? AND status = 'pending_approval'`,
		reason,
		reason,
		now,
		id,
	)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func hashApprovalContext(snapshot approvalContextSnapshot) (string, error) {
	snapshot.CapturedAt = ""
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("marshal approval context hash payload: %w", err)
	}
	return sha256Hex(string(payload)), nil
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func currentApprovalValidityProblem(snapshot approvalContextSnapshot, now time.Time) string {
	if snapshot.Token.RevokedAt != "" {
		return "API token was revoked after this request was created"
	}
	if expired(snapshot.Token.ExpiresAt, now) {
		return "API token expired after this request was created"
	}
	if expired(snapshot.Permission.ExpiresAt, now) {
		return "token permission expired after this request was created"
	}
	return ""
}

func expired(value string, now time.Time) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return false
	}
	return !parsed.After(now)
}

func describeApprovalContextDrift(stored approvalContextSnapshot, current approvalContextSnapshot) string {
	changes := []string{}
	if stored.Command.SHA256 != current.Command.SHA256 {
		changes = append(changes, "command payload changed")
	}
	if stored.Tool != current.Tool {
		changes = append(changes, "MCP tool metadata changed")
	}
	if stored.Token != current.Token {
		changes = append(changes, "API token changed")
	}
	if stored.Permission != current.Permission {
		changes = append(changes, "token permission changed")
	}
	if stored.Server != current.Server {
		changes = append(changes, "server profile or SSH key changed")
	}
	if len(changes) == 0 {
		return "approval context changed"
	}
	return "approval context changed: " + strings.Join(changes, ", ")
}
