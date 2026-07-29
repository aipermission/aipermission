package projects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const UngroupedSlug = "ungrouped"

var (
	ErrNotFound        = errors.New("project not found")
	ErrProtected       = errors.New("ungrouped project cannot be archived")
	ErrProjectNotEmpty = errors.New("move connector targets before archiving this project")
)

type ValidationError string

func (e ValidationError) Error() string { return string(e) }

type Store struct {
	db *sql.DB
}

type Project struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	TargetCount int    `json:"target_count"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type TokenScope struct {
	ProjectID   int64  `json:"project_id"`
	ProjectName string `json:"project_name"`
	ProjectSlug string `json:"project_slug"`
	Enabled     bool   `json:"enabled"`
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) List(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.slug, COUNT(ct.id), p.created_at, p.updated_at
		FROM projects p
		LEFT JOIN connector_targets ct ON ct.project_id = p.id AND ct.status = 'active'
		WHERE p.status = 'active'
		GROUP BY p.id
		ORDER BY CASE WHEN p.slug = ? THEN 1 ELSE 0 END, lower(p.name), p.id`, UngroupedSlug)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	items := []Project{}
	for rows.Next() {
		var item Project
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.TargetCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Get(ctx context.Context, id int64) (Project, error) {
	var item Project
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.slug, COUNT(ct.id), p.created_at, p.updated_at
		FROM projects p
		LEFT JOIN connector_targets ct ON ct.project_id = p.id AND ct.status = 'active'
		WHERE p.id = ? AND p.status = 'active'
		GROUP BY p.id`, id).Scan(&item.ID, &item.Name, &item.Slug, &item.TargetCount, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("get project: %w", err)
	}
	return item, nil
}

func (s *Store) Ungrouped(ctx context.Context) (Project, error) {
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM projects WHERE slug = ? AND status = 'active'`, UngroupedSlug).Scan(&id); err != nil {
		return Project{}, fmt.Errorf("get ungrouped project: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) Create(ctx context.Context, name string) (Project, error) {
	name = strings.TrimSpace(name)
	if err := validateName(name); err != nil {
		return Project{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin create project: %w", err)
	}
	defer tx.Rollback()
	slug, err := availableSlug(ctx, tx, slugify(name))
	if err != nil {
		return Project{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.ExecContext(ctx, `INSERT INTO projects (name, slug, status, created_at, updated_at) VALUES (?, ?, 'active', ?, ?)`, name, slug, now, now)
	if err != nil {
		if isUniqueError(err) {
			return Project{}, ValidationError("project name already exists")
		}
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Project{}, fmt.Errorf("read project id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO token_project_scopes (token_id, project_id, enabled, created_at, updated_at)
		SELECT id, ?, 1, ?, ? FROM api_tokens`, id, now, now); err != nil {
		return Project{}, fmt.Errorf("initialize token project scopes: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit create project: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) Update(ctx context.Context, id int64, name string) (Project, error) {
	name = strings.TrimSpace(name)
	if err := validateName(name); err != nil {
		return Project{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE projects SET name = ?, updated_at = ? WHERE id = ? AND status = 'active'`, name, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		if isUniqueError(err) {
			return Project{}, ValidationError("project name already exists")
		}
		return Project{}, fmt.Errorf("update project: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Project{}, err
	}
	if affected == 0 {
		return Project{}, ErrNotFound
	}
	return s.Get(ctx, id)
}

func (s *Store) Archive(ctx context.Context, id int64) error {
	item, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if item.Slug == UngroupedSlug {
		return ErrProtected
	}
	if item.TargetCount > 0 {
		return ErrProjectNotEmpty
	}
	var vaultReferences int
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM vault_items WHERE owner_project_id = ? AND status = 'active') +
			(SELECT COUNT(*) FROM vault_item_projects vip JOIN vault_items vi ON vi.id = vip.vault_item_id WHERE vip.project_id = ? AND vi.status = 'active')`,
		id, id,
	).Scan(&vaultReferences); err != nil {
		return fmt.Errorf("check project vault references: %w", err)
	}
	if vaultReferences > 0 {
		return ErrProjectNotEmpty
	}
	result, err := s.db.ExecContext(ctx, `UPDATE projects SET status = 'archived', updated_at = ? WHERE id = ? AND status = 'active'`, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("archive project: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListTokenScopes(ctx context.Context, tokenID int64) ([]TokenScope, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.slug, COALESCE(s.enabled, 0)
		FROM projects p
		LEFT JOIN token_project_scopes s ON s.project_id = p.id AND s.token_id = ?
		WHERE p.status = 'active'
		ORDER BY CASE WHEN p.slug = ? THEN 1 ELSE 0 END, lower(p.name), p.id`, tokenID, UngroupedSlug)
	if err != nil {
		return nil, fmt.Errorf("list token project scopes: %w", err)
	}
	defer rows.Close()
	items := []TokenScope{}
	for rows.Next() {
		var item TokenScope
		var enabled int
		if err := rows.Scan(&item.ProjectID, &item.ProjectName, &item.ProjectSlug, &enabled); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ReplaceTokenScopes(ctx context.Context, tokenID int64, enabledProjectIDs []int64) ([]TokenScope, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin replace token project scopes: %w", err)
	}
	defer tx.Rollback()
	var tokenExists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM api_tokens WHERE id = ?`, tokenID).Scan(&tokenExists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ValidationError("token not found")
		}
		return nil, err
	}
	enabled := map[int64]bool{}
	for _, id := range enabledProjectIDs {
		if id < 1 || enabled[id] {
			return nil, ValidationError("project ids must be unique positive integers")
		}
		enabled[id] = true
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM projects WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	activeIDs := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		activeIDs = append(activeIDs, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(enabled) > len(activeIDs) {
		return nil, ValidationError("project not found")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range activeIDs {
		value := 0
		if enabled[id] {
			value = 1
			delete(enabled, id)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO token_project_scopes (token_id, project_id, enabled, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(token_id, project_id) DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at`, tokenID, id, value, now, now); err != nil {
			return nil, err
		}
	}
	if len(enabled) > 0 {
		return nil, ValidationError("project not found")
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit replace token project scopes: %w", err)
	}
	return s.ListTokenScopes(ctx, tokenID)
}

func (s *Store) ReplaceTokenScopesWithChange(ctx context.Context, tokenID int64, enabledProjectIDs []int64) ([]TokenScope, bool, error) {
	var tokenExists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM api_tokens WHERE id = ?`, tokenID).Scan(&tokenExists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, ValidationError("token not found")
		}
		return nil, false, err
	}
	before, err := s.ListTokenScopes(ctx, tokenID)
	if err != nil {
		return nil, false, err
	}
	desired := make(map[int64]bool, len(enabledProjectIDs))
	for _, projectID := range enabledProjectIDs {
		if projectID < 1 || desired[projectID] {
			return nil, false, ValidationError("project ids must be unique positive integers")
		}
		desired[projectID] = true
	}
	unchanged := true
	for _, item := range before {
		if item.Enabled != desired[item.ProjectID] {
			unchanged = false
		}
		delete(desired, item.ProjectID)
	}
	if unchanged && len(desired) == 0 {
		return before, false, nil
	}
	items, err := s.ReplaceTokenScopes(ctx, tokenID, enabledProjectIDs)
	if err != nil {
		return nil, false, err
	}
	return items, true, nil
}

func (s *Store) TokenCanAccessProject(ctx context.Context, tokenID int64, projectID int64) (bool, error) {
	var enabled int
	err := s.db.QueryRowContext(ctx, `
		SELECT s.enabled
		FROM token_project_scopes s
		JOIN projects p ON p.id = s.project_id AND p.status = 'active'
		WHERE s.token_id = ? AND s.project_id = ?`, tokenID, projectID).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return enabled == 1, nil
}

func validateName(name string) error {
	if name == "" {
		return ValidationError("project name is required")
	}
	if len(name) > 80 {
		return ValidationError("project name must be 80 characters or fewer")
	}
	return nil
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if builder.Len() > 0 && !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return "project"
	}
	return slug
}

func availableSlug(ctx context.Context, tx *sql.Tx, base string) (string, error) {
	for index := 1; index < 10000; index++ {
		candidate := base
		if index > 1 {
			candidate = fmt.Sprintf("%s-%d", base, index)
		}
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE slug = ?`, candidate).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", ValidationError("could not create a unique project slug")
}

func isUniqueError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}
