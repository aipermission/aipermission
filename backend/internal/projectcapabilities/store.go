package projectcapabilities

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	VaultMetadataRead = "vault.metadata.read"
	VaultItemGenerate = "vault.item.generate"
	VaultSessionApply = "vault.session.apply"

	RuleAlwaysRun        = "always_run"
	RuleApprovalRequired = "approval_required"
	RuleBlocked          = "blocked"
)

var (
	ErrNotFound = errors.New("project capability not found")
)

type ValidationError string

func (e ValidationError) Error() string { return string(e) }

type Definition struct {
	Name         string   `json:"name"`
	Label        string   `json:"label"`
	Description  string   `json:"description"`
	AllowedRules []string `json:"allowed_rules"`
}

type Capability struct {
	TokenID        int64  `json:"token_id"`
	ProjectID      int64  `json:"project_id"`
	ProjectName    string `json:"project_name"`
	ProjectSlug    string `json:"project_slug"`
	ProjectEnabled bool   `json:"project_enabled"`
	Name           string `json:"capability_name"`
	ExecutionRule  string `json:"execution_rule"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	Revision       int64  `json:"revision"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type SetInput struct {
	ProjectID     int64
	Name          string
	ExecutionRule string
	ExpiresAt     string
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func Definitions() []Definition {
	return []Definition{
		{
			Name:         VaultMetadataRead,
			Label:        "Read metadata",
			Description:  "List secret names and bounded non-secret metadata for this project.",
			AllowedRules: []string{RuleBlocked, RuleAlwaysRun},
		},
		{
			Name:         VaultItemGenerate,
			Label:        "Generate items",
			Description:  "Generate and store a new secret value without returning it to the agent.",
			AllowedRules: []string{RuleBlocked, RuleApprovalRequired},
		},
		{
			Name:         VaultSessionApply,
			Label:        "Apply to sessions",
			Description:  "Restart an eligible connector session with approved Vault items in its environment.",
			AllowedRules: []string{RuleBlocked, RuleApprovalRequired},
		},
	}
}

func DefinitionFor(name string) (Definition, bool) {
	for _, definition := range Definitions() {
		if definition.Name == name {
			return definition, true
		}
	}
	return Definition{}, false
}

func (s *Store) List(ctx context.Context, tokenID int64) ([]Capability, error) {
	if tokenID < 1 {
		return nil, ValidationError("token id must be a positive integer")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.token_id, c.project_id, p.name, p.slug, COALESCE(ps.enabled, 0),
		       c.capability_name, c.execution_rule, COALESCE(c.expires_at, ''),
		       c.revision, c.created_at, c.updated_at
		FROM token_project_capabilities c
		JOIN projects p ON p.id = c.project_id AND p.status = 'active'
		LEFT JOIN token_project_scopes ps ON ps.token_id = c.token_id AND ps.project_id = c.project_id
		WHERE c.token_id = ?
		ORDER BY lower(p.name), p.id, c.capability_name`, tokenID)
	if err != nil {
		return nil, fmt.Errorf("list project capabilities: %w", err)
	}
	defer rows.Close()
	items := []Capability{}
	for rows.Next() {
		item, err := scanCapability(rows)
		if err != nil {
			return nil, err
		}
		if _, supported := DefinitionFor(item.Name); supported {
			items = append(items, item)
		}
	}
	return items, rows.Err()
}

func (s *Store) Effective(ctx context.Context, tokenID, projectID int64, name string, now time.Time) (Capability, error) {
	item, err := s.get(ctx, tokenID, projectID, name)
	if err != nil {
		return Capability{}, err
	}
	if !item.ProjectEnabled {
		return Capability{}, ErrNotFound
	}
	if item.ExpiresAt != "" {
		expiresAt, parseErr := time.Parse(time.RFC3339, item.ExpiresAt)
		if parseErr != nil || !expiresAt.After(now.UTC()) {
			return Capability{}, ErrNotFound
		}
	}
	return item, nil
}

func (s *Store) Replace(ctx context.Context, tokenID int64, inputs []SetInput) ([]Capability, error) {
	if tokenID < 1 {
		return nil, ValidationError("token id must be a positive integer")
	}
	normalized := make([]SetInput, 0, len(inputs))
	seen := map[string]bool{}
	for _, input := range inputs {
		value, err := normalizeInput(input)
		if err != nil {
			return nil, err
		}
		key := capabilityKey(value.ProjectID, value.Name)
		if seen[key] {
			return nil, ValidationError("project capabilities must be unique per project and capability")
		}
		seen[key] = true
		normalized = append(normalized, value)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin replace project capabilities: %w", err)
	}
	defer tx.Rollback()
	if err := requireToken(ctx, tx, tokenID); err != nil {
		return nil, err
	}
	for _, input := range normalized {
		if err := requireActiveProject(ctx, tx, input.ProjectID); err != nil {
			return nil, err
		}
	}
	createdAtByCapability, err := existingCreatedAt(ctx, tx, tokenID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, input := range normalized {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO token_project_capability_revisions (
				token_id, project_id, capability_name, revision, updated_at
			)
			VALUES (?, ?, ?, 1, ?)
			ON CONFLICT(token_id, project_id, capability_name)
			DO UPDATE SET revision = revision + 1, updated_at = excluded.updated_at`,
			tokenID, input.ProjectID, input.Name, now,
		); err != nil {
			return nil, fmt.Errorf("advance project capability revision: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM token_project_capabilities WHERE token_id = ?`, tokenID); err != nil {
		return nil, fmt.Errorf("clear project capabilities: %w", err)
	}
	for _, input := range normalized {
		createdAt := createdAtByCapability[capabilityKey(input.ProjectID, input.Name)]
		if createdAt == "" {
			createdAt = now
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO token_project_capabilities (
				token_id, project_id, capability_name, execution_rule, expires_at,
				revision, created_at, updated_at
			)
			SELECT ?, ?, ?, ?, NULLIF(?, ''), r.revision, ?, ?
			FROM token_project_capability_revisions r
			WHERE r.token_id = ? AND r.project_id = ? AND r.capability_name = ?
			ON CONFLICT(token_id, project_id, capability_name) DO UPDATE SET
				execution_rule = excluded.execution_rule,
				expires_at = excluded.expires_at,
				revision = excluded.revision,
				updated_at = excluded.updated_at`,
			tokenID, input.ProjectID, input.Name, input.ExecutionRule, input.ExpiresAt, createdAt, now,
			tokenID, input.ProjectID, input.Name,
		); err != nil {
			return nil, fmt.Errorf("insert project capability: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit project capabilities: %w", err)
	}
	return s.List(ctx, tokenID)
}

func existingCreatedAt(ctx context.Context, tx *sql.Tx, tokenID int64) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT project_id, capability_name, created_at
		FROM token_project_capabilities
		WHERE token_id = ?`, tokenID)
	if err != nil {
		return nil, fmt.Errorf("list existing project capabilities: %w", err)
	}
	values := map[string]string{}
	for rows.Next() {
		var projectID int64
		var name, createdAt string
		if err := rows.Scan(&projectID, &name, &createdAt); err != nil {
			rows.Close()
			return nil, err
		}
		values[capabilityKey(projectID, name)] = createdAt
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return values, nil
}

func capabilityKey(projectID int64, name string) string {
	return fmt.Sprintf("%d:%s", projectID, name)
}

func normalizeInput(input SetInput) (SetInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.ExecutionRule = strings.TrimSpace(input.ExecutionRule)
	input.ExpiresAt = strings.TrimSpace(input.ExpiresAt)
	if input.ProjectID < 1 {
		return SetInput{}, ValidationError("project id must be a positive integer")
	}
	definition, ok := DefinitionFor(input.Name)
	if !ok {
		return SetInput{}, ValidationError("unsupported project capability")
	}
	allowed := false
	for _, rule := range definition.AllowedRules {
		if input.ExecutionRule == rule {
			allowed = true
			break
		}
	}
	if !allowed {
		return SetInput{}, ValidationError("execution rule is not allowed for this project capability")
	}
	if input.ExecutionRule == RuleBlocked && input.ExpiresAt != "" {
		return SetInput{}, ValidationError("expires_at is not supported for blocked capabilities")
	}
	if input.ExpiresAt != "" {
		expiresAt, err := time.Parse(time.RFC3339, input.ExpiresAt)
		if err != nil {
			return SetInput{}, ValidationError("expires_at must be an RFC3339 timestamp")
		}
		if !expiresAt.After(time.Now().UTC()) {
			return SetInput{}, ValidationError("expires_at must be in the future")
		}
		input.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
	}
	return input, nil
}

func (s *Store) get(ctx context.Context, tokenID, projectID int64, name string) (Capability, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT c.token_id, c.project_id, p.name, p.slug, COALESCE(ps.enabled, 0),
		       c.capability_name, c.execution_rule, COALESCE(c.expires_at, ''),
		       c.revision, c.created_at, c.updated_at
		FROM token_project_capabilities c
		JOIN projects p ON p.id = c.project_id AND p.status = 'active'
		LEFT JOIN token_project_scopes ps ON ps.token_id = c.token_id AND ps.project_id = c.project_id
		WHERE c.token_id = ? AND c.project_id = ? AND c.capability_name = ?`,
		tokenID, projectID, strings.TrimSpace(name),
	)
	item, err := scanCapability(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Capability{}, ErrNotFound
	}
	return item, err
}

type scanner interface{ Scan(...any) error }

func scanCapability(row scanner) (Capability, error) {
	var item Capability
	var enabled int
	if err := row.Scan(
		&item.TokenID, &item.ProjectID, &item.ProjectName, &item.ProjectSlug, &enabled,
		&item.Name, &item.ExecutionRule, &item.ExpiresAt, &item.Revision,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return Capability{}, err
	}
	item.ProjectEnabled = enabled == 1
	return item, nil
}

func requireToken(ctx context.Context, tx *sql.Tx, tokenID int64) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM api_tokens WHERE id = ?`, tokenID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ValidationError("token not found")
		}
		return err
	}
	return nil
}

func requireActiveProject(ctx context.Context, tx *sql.Tx, projectID int64) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ? AND status = 'active'`, projectID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ValidationError("project not found")
		}
		return err
	}
	return nil
}
