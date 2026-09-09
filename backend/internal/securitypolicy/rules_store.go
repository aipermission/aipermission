package securitypolicy

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

const (
	maxRules        = 50
	maxRuleName     = 80
	maxPatternBytes = 500
)

func normalizeRuleInput(input RuleInput) RuleInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Pattern = strings.TrimSpace(input.Pattern)
	return input
}

func validateRuleInput(input RuleInput) error {
	if input.Name == "" {
		return ValidationError("name is required")
	}
	if len(input.Name) > maxRuleName {
		return ValidationError(fmt.Sprintf("name must be %d bytes or fewer", maxRuleName))
	}
	if input.Pattern == "" {
		return ValidationError("pattern is required")
	}
	if len(input.Pattern) > maxPatternBytes {
		return ValidationError(fmt.Sprintf("pattern must be %d bytes or fewer", maxPatternBytes))
	}
	if _, err := regexp.Compile(input.Pattern); err != nil {
		return ValidationError(fmt.Sprintf("pattern is not valid Go RE2 regex: %v", err))
	}
	return nil
}

func listRules(ctx context.Context, executor sqldb.Executor, enabledOnly bool) ([]Rule, error) {
	query := `SELECT id, name, pattern, enabled, created_at, updated_at FROM redaction_rules`
	if enabledOnly {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY name COLLATE NOCASE, id`
	rows, err := executor.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Rule{}
	for rows.Next() {
		item, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func insertRule(ctx context.Context, executor sqldb.Executor, input RuleInput) (Rule, error) {
	var count int
	if err := executor.QueryRowContext(ctx, `SELECT COUNT(*) FROM redaction_rules`).Scan(&count); err != nil {
		return Rule{}, err
	}
	if count >= maxRules {
		return Rule{}, ValidationError(fmt.Sprintf("redaction rule limit is %d", maxRules))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := executor.ExecContext(ctx, `
		INSERT INTO redaction_rules (name, pattern, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`, input.Name, input.Pattern, boolInt(input.Enabled), now, now)
	if err != nil {
		return Rule{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Rule{}, err
	}
	return getRule(ctx, executor, id)
}

func updateRule(ctx context.Context, executor sqldb.Executor, id int64, input RuleInput) (Rule, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := executor.ExecContext(ctx, `
		UPDATE redaction_rules
		SET name = ?, pattern = ?, enabled = ?, updated_at = ?
		WHERE id = ?`, input.Name, input.Pattern, boolInt(input.Enabled), now, id)
	if err != nil {
		return Rule{}, err
	}
	affected, err := sqldb.RowsAffected(result, "update redaction rule")
	if err != nil {
		return Rule{}, err
	}
	if affected == 0 {
		return Rule{}, sql.ErrNoRows
	}
	return getRule(ctx, executor, id)
}

func deleteRule(ctx context.Context, executor sqldb.Executor, id int64) error {
	result, err := executor.ExecContext(ctx, `DELETE FROM redaction_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := sqldb.RowsAffected(result, "delete redaction rule")
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func getRule(ctx context.Context, executor sqldb.Executor, id int64) (Rule, error) {
	return scanRule(executor.QueryRowContext(ctx, `
		SELECT id, name, pattern, enabled, created_at, updated_at
		FROM redaction_rules WHERE id = ?`, id))
}

func scanRule(scanner interface{ Scan(...any) error }) (Rule, error) {
	var item Rule
	var enabled int
	if err := scanner.Scan(&item.ID, &item.Name, &item.Pattern, &enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Rule{}, err
	}
	item.Enabled = enabled != 0
	return item, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
