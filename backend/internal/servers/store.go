package servers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type Server struct {
	ID                       int64  `json:"id"`
	Name                     string `json:"name"`
	Host                     string `json:"host"`
	Port                     int    `json:"port"`
	Username                 string `json:"username"`
	SSHKeyID                 int64  `json:"ssh_key_id"`
	SSHKeyName               string `json:"ssh_key_name,omitempty"`
	Description              string `json:"description"`
	StartupInputAfterConnect string `json:"startup_input_after_connect,omitempty"`
	ForceShellCommand        string `json:"force_shell_command,omitempty"`
	CreatedAt                string `json:"created_at"`
	UpdatedAt                string `json:"updated_at"`
}

type CreateRequest struct {
	Name                     string `json:"name"`
	Host                     string `json:"host"`
	Port                     int    `json:"port"`
	Username                 string `json:"username"`
	SSHKeyID                 int64  `json:"ssh_key_id"`
	Description              string `json:"description"`
	StartupInputAfterConnect string `json:"startup_input_after_connect"`
	ForceShellCommand        string `json:"force_shell_command"`
}

type UpdateRequest = CreateRequest

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) List(ctx context.Context) ([]Server, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id, s.name, s.host, s.port, s.username, s.ssh_key_id, COALESCE(k.name, ''), s.description, s.startup_input_after_connect, s.force_shell_command, s.created_at, s.updated_at FROM servers s LEFT JOIN ssh_keys k ON k.id = s.ssh_key_id ORDER BY s.name`)
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	defer rows.Close()

	items := []Server{}
	for rows.Next() {
		var item Server
		if err := rows.Scan(&item.ID, &item.Name, &item.Host, &item.Port, &item.Username, &item.SSHKeyID, &item.SSHKeyName, &item.Description, &item.StartupInputAfterConnect, &item.ForceShellCommand, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan server: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate servers: %w", err)
	}
	return items, nil
}

func (s *Store) Get(ctx context.Context, id int64) (Server, error) {
	var item Server
	err := s.db.QueryRowContext(ctx, `SELECT s.id, s.name, s.host, s.port, s.username, s.ssh_key_id, COALESCE(k.name, ''), s.description, s.startup_input_after_connect, s.force_shell_command, s.created_at, s.updated_at FROM servers s LEFT JOIN ssh_keys k ON k.id = s.ssh_key_id WHERE s.id = ?`, id).
		Scan(&item.ID, &item.Name, &item.Host, &item.Port, &item.Username, &item.SSHKeyID, &item.SSHKeyName, &item.Description, &item.StartupInputAfterConnect, &item.ForceShellCommand, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Server{}, ErrNotFound
	}
	if err != nil {
		return Server{}, fmt.Errorf("get server: %w", err)
	}
	return item, nil
}

func (s *Store) Create(ctx context.Context, request CreateRequest) (Server, error) {
	normalized, err := normalizeRequest(ctx, s.db, request)
	if err != nil {
		return Server{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Server{}, fmt.Errorf("begin create server: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(
		ctx,
		`INSERT INTO servers (name, host, port, username, ssh_key_id, auth_type, key_label, encrypted_secret, description, startup_input_after_connect, force_shell_command, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normalized.Name,
		normalized.Host,
		normalized.Port,
		normalized.Username,
		normalized.SSHKeyID,
		"private_key",
		"",
		"gateway-managed-ssh-key",
		normalized.Description,
		normalized.StartupInputAfterConnect,
		normalized.ForceShellCommand,
		now,
		now,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return Server{}, ValidationError("server name already exists")
		}
		return Server{}, fmt.Errorf("create server: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Server{}, fmt.Errorf("read server id: %w", err)
	}
	if _, err := connectortargets.SyncSSHServerByID(ctx, tx, id); err != nil {
		return Server{}, fmt.Errorf("sync server connector target: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Server{}, fmt.Errorf("commit create server: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) Update(ctx context.Context, id int64, request UpdateRequest) (Server, error) {
	normalized, err := normalizeRequest(ctx, s.db, request)
	if err != nil {
		return Server{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Server{}, fmt.Errorf("begin update server: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(
		ctx,
		`UPDATE servers SET name = ?, host = ?, port = ?, username = ?, ssh_key_id = ?, auth_type = ?, key_label = ?, encrypted_secret = ?, description = ?, startup_input_after_connect = ?, force_shell_command = ?, updated_at = ? WHERE id = ?`,
		normalized.Name,
		normalized.Host,
		normalized.Port,
		normalized.Username,
		normalized.SSHKeyID,
		"private_key",
		"",
		"gateway-managed-ssh-key",
		normalized.Description,
		normalized.StartupInputAfterConnect,
		normalized.ForceShellCommand,
		now,
		id,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return Server{}, ValidationError("server name already exists")
		}
		return Server{}, fmt.Errorf("update server: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return Server{}, fmt.Errorf("read rows affected: %w", err)
	}
	if affected == 0 {
		return Server{}, ErrNotFound
	}
	if _, err := connectortargets.SyncSSHServerByID(ctx, tx, id); err != nil {
		return Server{}, fmt.Errorf("sync server connector target: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Server{}, fmt.Errorf("commit update server: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete server: %w", err)
	}
	defer tx.Rollback()

	if err := connectortargets.DeleteSSHServerMapping(ctx, tx, id); err != nil {
		return fmt.Errorf("delete server connector target: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM servers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete server: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete server: %w", err)
	}
	return nil
}

func (s *Store) SyncConnectorTargets(ctx context.Context) error {
	return connectortargets.NewStore(s.db).SyncSSHServers(ctx)
}

func normalizeRequest(ctx context.Context, db *sql.DB, request CreateRequest) (CreateRequest, error) {
	request.Name = strings.TrimSpace(request.Name)
	request.Host = strings.TrimSpace(request.Host)
	request.Username = strings.TrimSpace(request.Username)
	request.Description = strings.TrimSpace(request.Description)
	request.StartupInputAfterConnect = normalizeStartupInput(request.StartupInputAfterConnect)
	request.ForceShellCommand = strings.TrimSpace(request.ForceShellCommand)

	if request.Port == 0 {
		request.Port = 22
	}

	if request.Name == "" {
		return request, ValidationError("name is required")
	}
	if err := validateSingleLineField("name", request.Name, 80); err != nil {
		return request, err
	}
	if request.Host == "" {
		return request, ValidationError("host is required")
	}
	if err := ValidateHost(request.Host); err != nil {
		return request, err
	}
	if request.Port < 1 || request.Port > 65535 {
		return request, ValidationError("port must be between 1 and 65535")
	}
	if request.Username == "" {
		return request, ValidationError("username is required")
	}
	if err := validateSSHUsername(request.Username); err != nil {
		return request, err
	}
	if err := validateSingleLineField("description", request.Description, 1000); err != nil {
		return request, err
	}
	if err := validateStartupInput(request.StartupInputAfterConnect); err != nil {
		return request, err
	}
	if err := validateSingleLineField("force_shell_command", request.ForceShellCommand, 200); err != nil {
		return request, err
	}
	if request.SSHKeyID < 1 {
		return request, ValidationError("ssh_key_id is required")
	}

	var exists int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM ssh_keys WHERE id = ?`, request.SSHKeyID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return request, ValidationError("ssh_key_id does not exist")
	}
	if err != nil {
		return request, fmt.Errorf("validate ssh key: %w", err)
	}

	return request, nil
}

func normalizeStartupInput(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func validateStartupInput(value string) error {
	if len([]byte(value)) > 2000 {
		return ValidationError("startup_input_after_connect must be 2000 bytes or fewer")
	}
	for _, r := range value {
		if r == '\n' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) {
			return ValidationError("startup_input_after_connect may only contain printable text, tabs, and newlines")
		}
	}
	return nil
}

func ValidateHost(host string) error {
	if len([]rune(host)) > 255 {
		return ValidationError("host must be 255 characters or fewer")
	}
	if strings.Contains(host, "://") || strings.ContainsAny(host, "/\\") {
		return ValidationError("host must be a hostname or IP address, not a URL")
	}
	if strings.ContainsAny(host, " \t\r\n") {
		return ValidationError("host cannot contain whitespace")
	}
	for _, r := range host {
		if unicode.IsControl(r) {
			return ValidationError("host cannot contain control characters")
		}
	}
	return nil
}

func validateSSHUsername(username string) error {
	if len([]rune(username)) > 64 {
		return ValidationError("username must be 64 characters or fewer")
	}
	if strings.ContainsAny(username, " \t\r\n") {
		return ValidationError("username cannot contain whitespace")
	}
	for _, r := range username {
		if unicode.IsControl(r) {
			return ValidationError("username cannot contain control characters")
		}
	}
	return nil
}

func validateSingleLineField(name string, value string, maxRunes int) error {
	if len([]rune(value)) > maxRunes {
		return ValidationError(fmt.Sprintf("%s must be %d characters or fewer", name, maxRunes))
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ValidationError(fmt.Sprintf("%s must be printable and single-line", name))
		}
	}
	return nil
}

func isUniqueConstraintError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}
