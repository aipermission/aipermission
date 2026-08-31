package vaultrequests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

const (
	ActionGenerateItem   = "generate_item"
	ActionRestartSession = "restart_session_with_environment"

	ApprovalTTL            = 15 * time.Minute
	StatusApprovalPending  = "approval_pending"
	StatusRunning          = "running"
	StatusCompleted        = "completed"
	StatusFailed           = "failed"
	StatusDeclined         = "declined"
	StatusStale            = "stale"
	StatusCanceled         = "canceled"
	StatusExpired          = "expired"
	requestTimestampLayout = "2006-01-02T15:04:05.000000000Z07:00"
)

var (
	ErrNotFound            = errors.New("Vault action request not found")
	ErrNotPending          = errors.New("Vault action request is not pending")
	ErrIdempotencyConflict = errors.New("Vault idempotency key was already used for different input")
)

type Request struct {
	ID                  int64          `json:"id"`
	TokenID             int64          `json:"token_id"`
	TokenName           string         `json:"token_name"`
	ProjectID           int64          `json:"project_id"`
	ProjectName         string         `json:"project_name"`
	ProjectSlug         string         `json:"project_slug"`
	RuntimeID           *int64         `json:"runtime_id,omitempty"`
	ActionName          string         `json:"action_name"`
	Source              string         `json:"source"`
	Input               map[string]any `json:"input"`
	Reason              string         `json:"reason"`
	Status              string         `json:"status"`
	ApprovalContext     map[string]any `json:"approval_context,omitempty"`
	ApprovalContextHash string         `json:"approval_context_hash"`
	IdempotencyKey      string         `json:"idempotency_key"`
	Error               string         `json:"error,omitempty"`
	Output              any            `json:"output,omitempty"`
	UserNote            string         `json:"user_note,omitempty"`
	CreatedAt           string         `json:"created_at"`
	ExpiresAt           string         `json:"expires_at"`
	CompletedAt         *string        `json:"completed_at,omitempty"`
	UpdatedAt           string         `json:"updated_at"`
}

type CreateInput struct {
	TokenID             int64
	ProjectID           int64
	RuntimeID           *int64
	ActionName          string
	Input               map[string]any
	Reason              string
	ApprovalContext     map[string]any
	ApprovalContextHash string
	IdempotencyKey      string
	InitialStatus       string
}

type Store struct {
	db           sqldb.Executor
	mutationHook MutationHook
}

type storeDB = sqldb.Executor

type MutationHook func(context.Context, sqldb.Executor, Request) error

func NewStore(db *sql.DB) *Store   { return &Store{db: db} }
func NewTxStore(tx *sql.Tx) *Store { return &Store{db: tx} }

func (s *Store) WithMutationHook(hook MutationHook) *Store {
	if s != nil {
		s.mutationHook = hook
	}
	return s
}

func (s *Store) transaction(ctx context.Context, label string) (sqldb.Executor, func() error, func(), error) {
	return sqldb.Transaction(ctx, s.db, nil, label)
}

func (s *Store) Create(ctx context.Context, input CreateInput) (Request, bool, error) {
	input.ActionName = strings.TrimSpace(input.ActionName)
	input.Reason = strings.TrimSpace(input.Reason)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.TokenID < 1 || input.ProjectID < 1 || input.IdempotencyKey == "" {
		return Request{}, false, fmt.Errorf("token, project, and idempotency key are required")
	}
	if input.ActionName != ActionGenerateItem && input.ActionName != ActionRestartSession {
		return Request{}, false, fmt.Errorf("unsupported Vault action")
	}
	if input.InitialStatus == "" {
		input.InitialStatus = StatusApprovalPending
	}
	if input.InitialStatus != StatusApprovalPending && input.InitialStatus != StatusRunning {
		return Request{}, false, fmt.Errorf("unsupported initial Vault request status")
	}
	inputJSON, err := json.Marshal(nonNilMap(input.Input))
	if err != nil {
		return Request{}, false, fmt.Errorf("encode Vault action input: %w", err)
	}
	contextJSON, err := json.Marshal(nonNilMap(input.ApprovalContext))
	if err != nil {
		return Request{}, false, fmt.Errorf("encode Vault approval context: %w", err)
	}
	nowTime := time.Now().UTC()
	if err := s.ExpirePending(ctx, nowTime); err != nil {
		return Request{}, false, err
	}
	now := formatTimestamp(nowTime)
	expiresAt := formatTimestamp(nowTime.Add(ApprovalTTL))
	executor, commit, rollback, err := s.transaction(ctx, "Vault action request")
	if err != nil {
		return Request{}, false, err
	}
	defer rollback()
	result, err := executor.ExecContext(ctx, `
		INSERT INTO vault_action_requests (
			token_id, project_id, runtime_id, action_name, source, input_json, reason,
			status, approval_context_json, approval_context_hash, idempotency_key,
			created_at, expires_at, updated_at
		) VALUES (?, ?, ?, ?, 'mcp', ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(token_id, idempotency_key) DO NOTHING`,
		input.TokenID, input.ProjectID, input.RuntimeID, input.ActionName, string(inputJSON),
		input.Reason, input.InitialStatus, string(contextJSON), input.ApprovalContextHash,
		input.IdempotencyKey, now, expiresAt, now,
	)
	if err != nil {
		return Request{}, false, fmt.Errorf("create Vault action request: %w", err)
	}
	affected, err := sqldb.RowsAffected(result, "create Vault action request")
	if err != nil {
		return Request{}, false, err
	}
	item, err := scanRequest(executor.QueryRowContext(ctx, requestSelect+` WHERE r.token_id = ? AND r.idempotency_key = ?`, input.TokenID, input.IdempotencyKey))
	if err != nil {
		return Request{}, false, err
	}
	if affected != 1 && !sameCreateInput(item, input) {
		return Request{}, false, ErrIdempotencyConflict
	}
	if affected == 1 {
		if err := history.SyncVaultActionRequestWithExecutor(ctx, executor, item.ID); err != nil {
			return Request{}, false, err
		}
		if s.mutationHook != nil {
			if err := s.mutationHook(ctx, executor, item); err != nil {
				return Request{}, false, err
			}
		}
	}
	if err := commit(); err != nil {
		return Request{}, false, err
	}
	return item, affected == 1, nil
}

func (s *Store) Get(ctx context.Context, id int64) (Request, error) {
	if err := s.ExpirePending(ctx, time.Now().UTC()); err != nil {
		return Request{}, err
	}
	return scanRequest(s.db.QueryRowContext(ctx, requestSelect+` WHERE r.id = ?`, id))
}

func (s *Store) GetByIdempotencyKey(ctx context.Context, tokenID int64, key string) (Request, error) {
	if err := s.ExpirePending(ctx, time.Now().UTC()); err != nil {
		return Request{}, err
	}
	return scanRequest(s.db.QueryRowContext(ctx, requestSelect+` WHERE r.token_id = ? AND r.idempotency_key = ?`, tokenID, key))
}

func (s *Store) List(ctx context.Context, status string, limit int) ([]Request, error) {
	if err := s.ExpirePending(ctx, time.Now().UTC()); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		limit = 100
	}
	where := ""
	args := []any{}
	if strings.TrimSpace(status) != "" {
		where = " WHERE r.status = ?"
		args = append(args, strings.TrimSpace(status))
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, requestSelect+where+` ORDER BY r.created_at DESC, r.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("list Vault action requests: %w", err)
	}
	defer rows.Close()
	items := []Request{}
	for rows.Next() {
		item, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Claim(ctx context.Context, id int64) (Request, error) {
	nowTime := time.Now().UTC()
	if err := s.ExpirePending(ctx, nowTime); err != nil {
		return Request{}, err
	}
	now := formatTimestamp(nowTime)
	affected, err := s.mutateAndSync(ctx, id, func(executor storeDB) (sql.Result, error) {
		return executor.ExecContext(ctx, `
			UPDATE vault_action_requests
			SET status = ?, error = '', updated_at = ?
			WHERE id = ? AND status = ? AND julianday(expires_at) > julianday(?)`,
			StatusRunning, now, id, StatusApprovalPending, now,
		)
	})
	if err != nil {
		return Request{}, err
	}
	if affected != 1 {
		if _, err := s.Get(ctx, id); errors.Is(err, ErrNotFound) {
			return Request{}, ErrNotFound
		}
		return Request{}, ErrNotPending
	}
	item, err := s.getRaw(ctx, id)
	if err != nil {
		return Request{}, err
	}
	return item, nil
}

func (s *Store) Complete(ctx context.Context, id int64, status string, output any, errorText string, userNote string) (Request, error) {
	switch status {
	case StatusCompleted, StatusFailed, StatusDeclined, StatusStale:
	default:
		return Request{}, fmt.Errorf("invalid terminal Vault request status")
	}
	return s.transition(ctx, id, StatusRunning, status, output, errorText, userNote, true)
}

func (s *Store) Decline(ctx context.Context, id int64, userNote string) (Request, error) {
	if err := s.ExpirePending(ctx, time.Now().UTC()); err != nil {
		return Request{}, err
	}
	return s.transition(ctx, id, StatusApprovalPending, StatusDeclined, nil, "", userNote, true)
}

func (s *Store) CancelOwned(ctx context.Context, id, tokenID int64) (Request, error) {
	if err := s.ExpirePending(ctx, time.Now().UTC()); err != nil {
		return Request{}, err
	}
	now := formatTimestamp(time.Now().UTC())
	affected, err := s.mutateAndSync(ctx, id, func(executor storeDB) (sql.Result, error) {
		return executor.ExecContext(ctx, `
			UPDATE vault_action_requests
			SET status = ?, error = 'canceled by requesting token', completed_at = ?, updated_at = ?
			WHERE id = ? AND token_id = ? AND status = ?`,
			StatusCanceled, now, now, id, tokenID, StatusApprovalPending,
		)
	})
	if err != nil {
		return Request{}, err
	}
	if affected != 1 {
		var ownerTokenID int64
		var status string
		err := s.db.QueryRowContext(ctx, `
			SELECT token_id, status FROM vault_action_requests WHERE id = ?`, id,
		).Scan(&ownerTokenID, &status)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && ownerTokenID != tokenID) {
			return Request{}, ErrNotFound
		}
		if err != nil {
			return Request{}, err
		}
		return Request{}, ErrNotPending
	}
	item, err := s.getRaw(ctx, id)
	if err != nil {
		return Request{}, err
	}
	return item, nil
}

func (s *Store) ExpirePending(ctx context.Context, now time.Time) error {
	nowText := formatTimestamp(now)
	ids, err := s.collectIDs(ctx, `
		SELECT id FROM vault_action_requests
		WHERE status = ? AND julianday(expires_at) <= julianday(?)
		ORDER BY id`,
		StatusApprovalPending, nowText,
	)
	if err != nil {
		return err
	}
	for _, id := range ids {
		affected, err := s.mutateAndSync(ctx, id, func(executor storeDB) (sql.Result, error) {
			return executor.ExecContext(ctx, `
				UPDATE vault_action_requests
				SET status = ?, error = 'approval request expired', completed_at = ?, updated_at = ?
				WHERE id = ? AND status = ? AND julianday(expires_at) <= julianday(?)`,
				StatusExpired, nowText, nowText, id, StatusApprovalPending, nowText,
			)
		})
		if err != nil {
			return err
		}
		_ = affected
	}
	return nil
}

func (s *Store) FailRunning(ctx context.Context, errorText string) error {
	ids, err := s.collectIDs(ctx, `
		SELECT id FROM vault_action_requests
		WHERE status = ?
		ORDER BY id`, StatusRunning)
	if err != nil {
		return err
	}
	now := formatTimestamp(time.Now().UTC())
	for _, id := range ids {
		affected, err := s.mutateAndSync(ctx, id, func(executor storeDB) (sql.Result, error) {
			return executor.ExecContext(ctx, `
				UPDATE vault_action_requests
				SET status = ?, error = ?, completed_at = ?, updated_at = ?
				WHERE id = ? AND status = ?`,
				StatusFailed, strings.TrimSpace(errorText), now, now, id, StatusRunning,
			)
		})
		if err != nil {
			return err
		}
		_ = affected
	}
	return nil
}

func (s *Store) StalePendingForToken(ctx context.Context, tokenID int64, reason string) error {
	return s.stalePendingWhere(ctx, "token_id = ?", []any{tokenID}, reason)
}

func (s *Store) StalePendingForProject(ctx context.Context, projectID int64, reason string) error {
	return s.stalePendingWhere(ctx, "project_id = ?", []any{projectID}, reason)
}

func (s *Store) StalePendingForAction(ctx context.Context, actionName string, reason string) error {
	return s.stalePendingWhere(
		ctx, "action_name = ?", []any{strings.TrimSpace(actionName)}, reason,
	)
}

func (s *Store) StalePendingForRuntimes(ctx context.Context, runtimeIDs []int64, reason string) error {
	seen := map[int64]bool{}
	ids := []int64{}
	for _, runtimeID := range runtimeIDs {
		if runtimeID < 1 || seen[runtimeID] {
			continue
		}
		seen[runtimeID] = true
		ids = append(ids, runtimeID)
	}
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return s.stalePendingWhere(ctx, "runtime_id IN ("+placeholders+")", args, reason)
}

func (s *Store) StalePending(ctx context.Context, id int64, reason string) (Request, error) {
	return s.stalePending(ctx, id, reason)
}

func (s *Store) StalePendingForContext(ctx context.Context, itemID, bindingID int64, reason string) error {
	rows, err := s.db.QueryContext(ctx, requestSelect+` WHERE r.status = ? ORDER BY r.id`, StatusApprovalPending)
	if err != nil {
		return err
	}
	items := []Request{}
	for rows.Next() {
		item, err := scanRequest(rows)
		if err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		if !approvalContextReferences(item.ApprovalContext, itemID, bindingID) {
			continue
		}
		if _, err := s.stalePending(ctx, item.ID, reason); err != nil && !errors.Is(err, ErrNotPending) {
			return err
		}
	}
	return nil
}

func (s *Store) collectIDs(ctx context.Context, query string, args ...any) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) stalePendingWhere(ctx context.Context, where string, args []any, reason string) error {
	queryArgs := append(append([]any(nil), args...), StatusApprovalPending)
	ids, err := s.collectIDs(
		ctx,
		"SELECT id FROM vault_action_requests WHERE "+where+" AND status = ? ORDER BY id",
		queryArgs...,
	)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.stalePending(ctx, id, reason); err != nil && !errors.Is(err, ErrNotPending) {
			return err
		}
	}
	return nil
}

func (s *Store) transition(ctx context.Context, id int64, from, to string, output any, errorText string, userNote string, terminal bool) (Request, error) {
	now := formatTimestamp(time.Now().UTC())
	outputJSON, err := json.Marshal(output)
	if err != nil {
		return Request{}, fmt.Errorf("encode Vault action output: %w", err)
	}
	affected, err := s.mutateAndSync(ctx, id, func(executor storeDB) (sql.Result, error) {
		if terminal {
			return executor.ExecContext(ctx, `
				UPDATE vault_action_requests
				SET status = ?, output_json = ?, error = ?, user_note = ?, completed_at = ?, updated_at = ?
				WHERE id = ? AND status = ?`,
				to, string(outputJSON), strings.TrimSpace(errorText), strings.TrimSpace(userNote), now, now, id, from)
		}
		return executor.ExecContext(ctx, `
			UPDATE vault_action_requests SET status = ?, error = '', updated_at = ?
			WHERE id = ? AND status = ?`, to, now, id, from)
	})
	if err != nil {
		return Request{}, err
	}
	if affected != 1 {
		if _, err := s.Get(ctx, id); errors.Is(err, ErrNotFound) {
			return Request{}, ErrNotFound
		}
		return Request{}, ErrNotPending
	}
	item, err := s.getRaw(ctx, id)
	if err != nil {
		return Request{}, err
	}
	return item, nil
}

func (s *Store) stalePending(ctx context.Context, id int64, reason string) (Request, error) {
	now := formatTimestamp(time.Now().UTC())
	affected, err := s.mutateAndSync(ctx, id, func(executor storeDB) (sql.Result, error) {
		return executor.ExecContext(ctx, `
			UPDATE vault_action_requests
			SET status = ?, error = ?, completed_at = ?, updated_at = ?
			WHERE id = ? AND status = ?`,
			StatusStale, strings.TrimSpace(reason), now, now, id, StatusApprovalPending,
		)
	})
	if err != nil {
		return Request{}, err
	}
	if affected != 1 {
		return Request{}, ErrNotPending
	}
	item, err := s.getRaw(ctx, id)
	if err != nil {
		return Request{}, err
	}
	return item, nil
}

func (s *Store) mutateAndSync(
	ctx context.Context,
	id int64,
	mutate func(storeDB) (sql.Result, error),
) (int64, error) {
	executor, commit, rollback, err := s.transaction(ctx, "Vault action request mutation")
	if err != nil {
		return 0, err
	}
	defer rollback()
	result, err := mutate(executor)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 1 {
		if err := history.SyncVaultActionRequestWithExecutor(ctx, executor, id); err != nil {
			return 0, err
		}
		if s.mutationHook != nil {
			item, err := scanRequest(executor.QueryRowContext(ctx, requestSelect+` WHERE r.id = ?`, id))
			if err != nil {
				return 0, err
			}
			if err := s.mutationHook(ctx, executor, item); err != nil {
				return 0, err
			}
		}
	}
	if err := commit(); err != nil {
		return 0, err
	}
	return affected, nil
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Format(requestTimestampLayout)
}

const requestSelect = `
	SELECT r.id, r.token_id, t.name, r.project_id, p.name, p.slug, r.runtime_id,
	       r.action_name, r.source, r.input_json, r.reason, r.status,
	       r.approval_context_json, r.approval_context_hash, r.idempotency_key,
	       r.error, r.output_json, r.user_note, r.created_at, r.expires_at,
	       r.completed_at, r.updated_at
	FROM vault_action_requests r
	JOIN api_tokens t ON t.id = r.token_id
	JOIN projects p ON p.id = r.project_id`

type scanner interface{ Scan(...any) error }

func scanRequest(row scanner) (Request, error) {
	var item Request
	var inputJSON, contextJSON, outputJSON string
	err := row.Scan(
		&item.ID, &item.TokenID, &item.TokenName, &item.ProjectID, &item.ProjectName,
		&item.ProjectSlug, &item.RuntimeID, &item.ActionName, &item.Source, &inputJSON,
		&item.Reason, &item.Status, &contextJSON, &item.ApprovalContextHash,
		&item.IdempotencyKey, &item.Error, &outputJSON, &item.UserNote,
		&item.CreatedAt, &item.ExpiresAt, &item.CompletedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, err
	}
	if err := json.Unmarshal([]byte(inputJSON), &item.Input); err != nil {
		return Request{}, fmt.Errorf("decode Vault action input: %w", err)
	}
	if err := json.Unmarshal([]byte(contextJSON), &item.ApprovalContext); err != nil {
		return Request{}, fmt.Errorf("decode Vault approval context: %w", err)
	}
	if outputJSON != "" && outputJSON != "null" {
		if err := json.Unmarshal([]byte(outputJSON), &item.Output); err != nil {
			return Request{}, fmt.Errorf("decode Vault action output: %w", err)
		}
	}
	return item, nil
}

func (s *Store) getRaw(ctx context.Context, id int64) (Request, error) {
	return scanRequest(s.db.QueryRowContext(ctx, requestSelect+` WHERE r.id = ?`, id))
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func sameCreateInput(item Request, input CreateInput) bool {
	if item.ProjectID != input.ProjectID || item.ActionName != input.ActionName ||
		item.Reason != input.Reason {
		return false
	}
	if (item.RuntimeID == nil) != (input.RuntimeID == nil) {
		return false
	}
	if item.RuntimeID != nil && *item.RuntimeID != *input.RuntimeID {
		return false
	}
	itemJSON, itemErr := json.Marshal(item.Input)
	inputJSON, inputErr := json.Marshal(nonNilMap(input.Input))
	return itemErr == nil && inputErr == nil && string(itemJSON) == string(inputJSON)
}

func approvalContextReferences(context map[string]any, itemID, bindingID int64) bool {
	values, _ := context["items"].([]any)
	for _, value := range values {
		item, _ := value.(map[string]any)
		if itemID > 0 && jsonNumber(item["item_id"]) == itemID {
			return true
		}
		if bindingID > 0 && jsonNumber(item["binding_id"]) == bindingID {
			return true
		}
	}
	return false
}

func jsonNumber(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		return 0
	}
}
