package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

const (
	maxRedactionRules        = 50
	maxRedactionRuleName     = 80
	maxRedactionPatternBytes = 500
)

type redactionRule struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Pattern   string `json:"pattern"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type redactionRuleRequest struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Enabled bool   `json:"enabled"`
}

func (s redactionRuleHandlers) listRedactionRules(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	items, err := readRedactionRules(r.Context(), runtime, false)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s redactionRuleHandlers) createRedactionRule(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request redactionRuleRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request = normalizeRedactionRuleRequest(request)
	if err := validateRedactionRuleRequest(request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	count, err := countRedactionRules(r.Context(), runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	if count >= maxRedactionRules {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("redaction rule limit is %d", maxRedactionRules))
		return
	}
	var item redactionRule
	err = s.withAuditedMutation(
		r.Context(), runtime, "user", nil, 0, "settings.redaction_rule.created",
		func() any { return map[string]any{"id": item.ID, "name": item.Name, "enabled": item.Enabled} },
		func(tx *sql.Tx) error {
			var err error
			item, err = insertRedactionRuleWithExecutor(r.Context(), tx, request)
			return err
		},
	)
	if err != nil {
		handleRedactionRuleError(w, err)
		return
	}
	s.invalidateRedactionRules(runtime)
	writeJSON(w, http.StatusCreated, item)
}

func (s redactionRuleHandlers) updateRedactionRule(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request redactionRuleRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request = normalizeRedactionRuleRequest(request)
	if err := validateRedactionRuleRequest(request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var item redactionRule
	err := s.withAuditedMutation(
		r.Context(), runtime, "user", nil, 0, "settings.redaction_rule.updated",
		func() any { return map[string]any{"id": item.ID, "name": item.Name, "enabled": item.Enabled} },
		func(tx *sql.Tx) error {
			var err error
			item, err = updateRedactionRuleRecordWithExecutor(r.Context(), tx, id, request)
			return err
		},
	)
	if err != nil {
		handleRedactionRuleError(w, err)
		return
	}
	s.invalidateRedactionRules(runtime)
	writeJSON(w, http.StatusOK, item)
}

func (s redactionRuleHandlers) deleteRedactionRule(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	deleted := false
	err := s.withAuditedMutation(
		r.Context(), runtime, "user", nil, 0, "settings.redaction_rule.deleted",
		func() any { return map[string]any{"id": id} },
		func(tx *sql.Tx) error {
			var err error
			deleted, err = deleteRedactionRuleRecordWithExecutor(r.Context(), tx, id)
			if err == nil && !deleted {
				return sql.ErrNoRows
			}
			return err
		},
	)
	if err != nil {
		handleRedactionRuleError(w, err)
		return
	}
	s.invalidateRedactionRules(runtime)
	w.WriteHeader(http.StatusNoContent)
}

func normalizeRedactionRuleRequest(request redactionRuleRequest) redactionRuleRequest {
	request.Name = strings.TrimSpace(request.Name)
	request.Pattern = strings.TrimSpace(request.Pattern)
	return request
}

func validateRedactionRuleRequest(request redactionRuleRequest) error {
	if request.Name == "" {
		return fmt.Errorf("name is required")
	}
	if err := validateTextLimit("name", request.Name, maxRedactionRuleName); err != nil {
		return err
	}
	if request.Pattern == "" {
		return fmt.Errorf("pattern is required")
	}
	if err := validateTextLimit("pattern", request.Pattern, maxRedactionPatternBytes); err != nil {
		return err
	}
	if _, err := regexp.Compile(request.Pattern); err != nil {
		return fmt.Errorf("pattern is not valid Go RE2 regex: %v", err)
	}
	return nil
}

func readRedactionRules(ctx context.Context, runtime *databaseRuntime, enabledOnly bool) ([]redactionRule, error) {
	query := `SELECT id, name, pattern, enabled, created_at, updated_at FROM redaction_rules`
	if enabledOnly {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY name COLLATE NOCASE, id`
	rows, err := runtime.database.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []redactionRule{}
	for rows.Next() {
		item, err := scanRedactionRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func countRedactionRules(ctx context.Context, runtime *databaseRuntime) (int, error) {
	var count int
	err := runtime.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM redaction_rules`).Scan(&count)
	return count, err
}

func insertRedactionRule(ctx context.Context, runtime *databaseRuntime, request redactionRuleRequest) (redactionRule, error) {
	return insertRedactionRuleWithExecutor(ctx, runtime.database, request)
}

func insertRedactionRuleWithExecutor(ctx context.Context, executor sqldb.Executor, request redactionRuleRequest) (redactionRule, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := executor.ExecContext(ctx, `
		INSERT INTO redaction_rules (name, pattern, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`,
		request.Name,
		request.Pattern,
		boolToInt(request.Enabled),
		now,
		now,
	)
	if err != nil {
		return redactionRule{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return redactionRule{}, err
	}
	return getRedactionRuleWithExecutor(ctx, executor, id)
}

func updateRedactionRuleRecord(ctx context.Context, runtime *databaseRuntime, id int64, request redactionRuleRequest) (redactionRule, error) {
	return updateRedactionRuleRecordWithExecutor(ctx, runtime.database, id, request)
}

func updateRedactionRuleRecordWithExecutor(ctx context.Context, executor sqldb.Executor, id int64, request redactionRuleRequest) (redactionRule, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := executor.ExecContext(ctx, `
		UPDATE redaction_rules
		SET name = ?, pattern = ?, enabled = ?, updated_at = ?
		WHERE id = ?`,
		request.Name,
		request.Pattern,
		boolToInt(request.Enabled),
		now,
		id,
	)
	if err != nil {
		return redactionRule{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return redactionRule{}, err
	}
	if affected == 0 {
		return redactionRule{}, sql.ErrNoRows
	}
	return getRedactionRuleWithExecutor(ctx, executor, id)
}

func deleteRedactionRuleRecord(ctx context.Context, runtime *databaseRuntime, id int64) (bool, error) {
	return deleteRedactionRuleRecordWithExecutor(ctx, runtime.database, id)
}

func deleteRedactionRuleRecordWithExecutor(ctx context.Context, executor sqldb.Executor, id int64) (bool, error) {
	result, err := executor.ExecContext(ctx, `DELETE FROM redaction_rules WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func getRedactionRule(ctx context.Context, runtime *databaseRuntime, id int64) (redactionRule, error) {
	return getRedactionRuleWithExecutor(ctx, runtime.database, id)
}

func getRedactionRuleWithExecutor(ctx context.Context, executor sqldb.Executor, id int64) (redactionRule, error) {
	row := executor.QueryRowContext(ctx, `
		SELECT id, name, pattern, enabled, created_at, updated_at
		FROM redaction_rules
		WHERE id = ?`, id)
	return scanRedactionRule(row)
}

func scanRedactionRule(scanner interface {
	Scan(dest ...any) error
}) (redactionRule, error) {
	var item redactionRule
	var enabled int
	if err := scanner.Scan(&item.ID, &item.Name, &item.Pattern, &enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return redactionRule{}, err
	}
	item.Enabled = enabled != 0
	return item, nil
}

func handleRedactionRuleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "redaction rule not found")
	case strings.Contains(strings.ToLower(err.Error()), "unique"):
		writeError(w, http.StatusConflict, "redaction rule name already exists")
	default:
		writeInternalError(w)
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
