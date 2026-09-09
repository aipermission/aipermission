package history

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

const defaultLabelColor = "#0f766e"

var labelColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type LabelStore struct{ database sqldb.Executor }

func NewLabelStore(database sqldb.Executor) *LabelStore { return &LabelStore{database: database} }

func (s *LabelStore) All(ctx context.Context) ([]Label, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT id, name, color, created_at, updated_at
		FROM history_labels ORDER BY lower(name), id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := []Label{}
	for rows.Next() {
		var label Label
		if err := rows.Scan(&label.ID, &label.Name, &label.Color, &label.CreatedAt, &label.UpdatedAt); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

func (s *LabelStore) Get(ctx context.Context, id int64) (Label, error) {
	var label Label
	err := s.database.QueryRowContext(ctx, `
		SELECT id, name, color, created_at, updated_at
		FROM history_labels WHERE id = ?`, id,
	).Scan(&label.ID, &label.Name, &label.Color, &label.CreatedAt, &label.UpdatedAt)
	return label, err
}

func (s *LabelStore) CreateOrGet(ctx context.Context, name, color string) (Label, bool, error) {
	name, err := normalizeLabelName(name)
	if err != nil {
		return Label{}, false, err
	}
	color = normalizeLabelColor(color)
	result, err := s.database.ExecContext(ctx, `
		INSERT OR IGNORE INTO history_labels (name, color, created_at, updated_at)
		VALUES (?, ?, datetime('now'), datetime('now'))`, name, color)
	if err != nil {
		return Label{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Label{}, false, err
	}
	label, err := labelByName(ctx, s.database, name)
	return label, affected > 0, err
}

func (s *LabelStore) GetByName(ctx context.Context, name string) (Label, error) {
	name, err := normalizeLabelName(name)
	if err != nil {
		return Label{}, err
	}
	return labelByName(ctx, s.database, name)
}

func labelByName(ctx context.Context, executor sqldb.Executor, name string) (Label, error) {
	var label Label
	err := executor.QueryRowContext(ctx, `
		SELECT id, name, color, created_at, updated_at
		FROM history_labels WHERE name = ? COLLATE NOCASE`, name,
	).Scan(&label.ID, &label.Name, &label.Color, &label.CreatedAt, &label.UpdatedAt)
	return label, err
}

func (s *LabelStore) EntryExists(ctx context.Context, id int64) (bool, error) {
	var exists int
	err := s.database.QueryRowContext(ctx, `SELECT 1 FROM history_entries WHERE id = ?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *LabelStore) Delete(ctx context.Context, id int64) error {
	result, err := s.database.ExecContext(ctx, `DELETE FROM history_labels WHERE id = ?`, id)
	return requireChanged(result, err)
}

func (s *LabelStore) Attach(ctx context.Context, entryID, labelID int64) (bool, error) {
	result, err := s.database.ExecContext(ctx, `
		INSERT OR IGNORE INTO history_entry_labels (history_entry_id, label_id, created_at)
		VALUES (?, ?, datetime('now'))`, entryID, labelID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (s *LabelStore) Detach(ctx context.Context, entryID, labelID int64) error {
	result, err := s.database.ExecContext(ctx, `
		DELETE FROM history_entry_labels
		WHERE history_entry_id = ? AND label_id = ?`, entryID, labelID)
	return requireChanged(result, err)
}

func requireChanged(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func normalizeLabelName(name string) (string, error) {
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	if name == "" {
		return "", errors.New("label name is required")
	}
	if len([]byte(name)) > 80 {
		return "", errors.New("label name must be 80 bytes or less")
	}
	return name, nil
}

func normalizeLabelColor(color string) string {
	color = strings.TrimSpace(color)
	if !labelColorPattern.MatchString(color) {
		return defaultLabelColor
	}
	return strings.ToLower(color)
}
