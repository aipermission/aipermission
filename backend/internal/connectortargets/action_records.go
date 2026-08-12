package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/history"
)

type ActionPermissionRule string

const (
	ActionPermissionAlwaysRun        ActionPermissionRule = "always_run"
	ActionPermissionApprovalRequired ActionPermissionRule = "approval_required"
	ActionPermissionBlocked          ActionPermissionRule = "blocked"
)

type SetActionPermissionInput struct {
	TokenID       int64
	TargetID      int64
	ProfileID     int64
	ActionName    string
	ExecutionRule ActionPermissionRule
	ExpiresAt     *time.Time
}

type ActionPermission struct {
	TokenID        int64
	ProjectID      int64
	ProjectName    string
	ProjectSlug    string
	ProjectEnabled bool
	TargetID       int64
	TargetName     string
	ProfileID      int64
	ProfileLabel   string
	ConnectorKind  string
	ProfileKind    string
	ActionName     string
	ExecutionRule  ActionPermissionRule
	ExpiresAt      string
	CreatedAt      string
	UpdatedAt      string
}

type ActionRequest struct {
	ID                      int64
	TokenID                 *int64
	TokenName               string
	TargetID                int64
	TargetName              string
	ProfileID               int64
	ProfileLabel            string
	ConnectorKind           string
	ActionName              string
	Title                   string
	Summary                 string
	Preview                 map[string]any
	Source                  string
	Input                   map[string]any
	EncryptedPayloadJSON    string
	Reason                  string
	Status                  connectors.ResultStatus
	Output                  any
	DisplayText             string
	Error                   string
	ApprovalContext         string
	ApprovalContextHash     string
	ApprovalContextDrift    string
	IdempotencyKey          string
	IdempotencyIdentityHash string
	SessionID               *int64
	SessionGeneration       *int64
	CreatedAt               string
	CompletedAt             *string
}

type InsertActionRequestInput struct {
	TokenID                 *int64
	TargetID                int64
	ProfileID               int64
	ConnectorKind           string
	ActionName              string
	Title                   string
	Summary                 string
	Preview                 map[string]any
	Source                  string
	Input                   map[string]any
	EncryptedPayloadJSON    string
	Reason                  string
	Status                  connectors.ResultStatus
	ApprovalContext         string
	ApprovalContextHash     string
	IdempotencyKey          string
	IdempotencyIdentityHash string
}

type FinishActionRequestInput struct {
	ID              int64
	Status          connectors.ResultStatus
	Output          any
	DisplayText     string
	Error           string
	ApprovalDrift   string
	AllowedStatuses []connectors.ResultStatus
}

type StaleActionRequestsForTargetInput struct {
	TargetID       int64
	ProfileID      int64
	Error          string
	ApprovalDrift  string
	IncludeRunning bool
}

type StaleActionRequestsForTargetResult struct {
	IDs      []int64
	Affected int64
}

type ActionRequestFilter struct {
	Status string
	Limit  int
}

func (s *Store) SetActionPermission(ctx context.Context, input SetActionPermissionInput) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("connector target store is not configured")
	}
	if err := validateActionPermissionInput(input); err != nil {
		return err
	}
	// This store owns target/profile existence only. Action-catalog validation
	// belongs to the API/service layer because supported actions can depend on
	// connector metadata and target/profile public configuration.
	if err := s.requireActiveTargetProfile(ctx, input.TargetID, input.ProfileID); err != nil {
		return err
	}
	now := nowString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO token_connector_action_permissions (
			token_id, target_id, profile_id, action_name, execution_rule, expires_at,
			created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(token_id, target_id, profile_id, action_name) DO UPDATE SET
			execution_rule = excluded.execution_rule,
			expires_at = excluded.expires_at,
			updated_at = excluded.updated_at`,
		input.TokenID,
		input.TargetID,
		input.ProfileID,
		input.ActionName,
		input.ExecutionRule,
		actionPermissionExpiresAt(input),
		now,
		now,
	)
	return err
}

func (s *Store) GetActionPermission(ctx context.Context, tokenID int64, targetID int64, profileID int64, actionName string, now time.Time) (ActionPermission, error) {
	if s == nil || s.db == nil {
		return ActionPermission{}, fmt.Errorf("connector target store is not configured")
	}
	if tokenID < 1 || targetID < 1 || profileID < 1 || !connectors.ValidIdentifier(actionName) {
		return ActionPermission{}, ErrActionPermissionNotFound
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT
			p.token_id, t.project_id, project.name, project.slug, scope.enabled, p.target_id, t.name, p.profile_id, cp.label,
			t.connector_kind, cp.kind, p.action_name, p.execution_rule,
			COALESCE(p.expires_at, ''), p.created_at, p.updated_at
		FROM token_connector_action_permissions p
		JOIN connector_targets t ON t.id = p.target_id
		JOIN projects project ON project.id = t.project_id AND project.status = 'active'
		JOIN token_project_scopes scope ON scope.token_id = p.token_id AND scope.project_id = t.project_id AND scope.enabled = 1
		JOIN connector_credential_profiles cp ON cp.id = p.profile_id AND cp.target_id = p.target_id
		WHERE
			p.token_id = ?
			AND p.target_id = ?
				AND p.profile_id = ?
				AND p.action_name = ?
				AND t.status = 'active'
				AND cp.status = 'active'
				AND cp.connector_kind = t.connector_kind
				AND (COALESCE(p.expires_at, '') = '' OR p.expires_at > ?)`,
		tokenID,
		targetID,
		profileID,
		actionName,
		now.UTC().Format(time.RFC3339),
	)
	permission, err := scanActionPermission(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ActionPermission{}, ErrActionPermissionNotFound
	}
	if err != nil {
		return ActionPermission{}, err
	}
	return permission, nil
}

func (s *Store) ReplaceActionPermissions(ctx context.Context, tokenID int64, inputs []SetActionPermissionInput) ([]ActionPermission, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("connector target store is not configured")
	}
	if tokenID < 1 {
		return nil, ValidationError("token_id is required")
	}
	if err := s.validateActionPermissions(ctx, tokenID, inputs); err != nil {
		return nil, err
	}
	executor, commit, rollback, err := s.transaction(ctx, "connector permission update")
	if err != nil {
		return nil, err
	}
	defer rollback()

	if _, err := executor.ExecContext(ctx, `DELETE FROM token_connector_action_permissions WHERE token_id = ?`, tokenID); err != nil {
		return nil, fmt.Errorf("clear connector action permissions: %w", err)
	}
	now := nowString()
	for _, input := range inputs {
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO token_connector_action_permissions (
				token_id, target_id, profile_id, action_name, execution_rule, expires_at,
				created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			tokenID,
			input.TargetID,
			input.ProfileID,
			input.ActionName,
			input.ExecutionRule,
			actionPermissionExpiresAt(input),
			now,
			now,
		); err != nil {
			return nil, fmt.Errorf("insert connector action permission: %w", err)
		}
	}
	items, err := (&Store{db: executor}).ListActionPermissions(ctx, tokenID)
	if err != nil {
		return nil, err
	}
	if err := commit(); err != nil {
		return nil, fmt.Errorf("commit connector permission update: %w", err)
	}
	return items, nil
}

func (s *Store) ReplaceActionPermissionsWithChange(ctx context.Context, tokenID int64, inputs []SetActionPermissionInput) ([]ActionPermission, bool, error) {
	if err := s.validateActionPermissions(ctx, tokenID, inputs); err != nil {
		return nil, false, err
	}
	before, err := s.ListActionPermissions(ctx, tokenID)
	if err != nil {
		return nil, false, err
	}
	if actionPermissionInputsEqual(before, inputs) {
		return before, false, nil
	}
	items, err := s.ReplaceActionPermissions(ctx, tokenID, inputs)
	if err != nil {
		return nil, false, err
	}
	return items, true, nil
}

func actionPermissionInputsEqual(items []ActionPermission, inputs []SetActionPermissionInput) bool {
	if len(items) != len(inputs) {
		return false
	}
	type state struct {
		rule      ActionPermissionRule
		expiresAt string
	}
	values := make(map[string]state, len(items))
	for _, item := range items {
		key := fmt.Sprintf("%d:%d:%s", item.TargetID, item.ProfileID, item.ActionName)
		values[key] = state{rule: item.ExecutionRule, expiresAt: item.ExpiresAt}
	}
	for _, input := range inputs {
		key := fmt.Sprintf("%d:%d:%s", input.TargetID, input.ProfileID, strings.TrimSpace(input.ActionName))
		current, ok := values[key]
		if !ok || current.rule != input.ExecutionRule || current.expiresAt != actionPermissionExpiresAtValue(input) {
			return false
		}
		delete(values, key)
	}
	return len(values) == 0
}

func (s *Store) ListActionPermissions(ctx context.Context, tokenID int64) ([]ActionPermission, error) {
	return s.ListActiveActionPermissions(ctx, tokenID, time.Now().UTC())
}

func (s *Store) ListActiveActionPermissions(ctx context.Context, tokenID int64, now time.Time) ([]ActionPermission, error) {
	return s.listActionPermissions(ctx, tokenID, now, false)
}

func (s *Store) ListScopedActionPermissions(ctx context.Context, tokenID int64, now time.Time) ([]ActionPermission, error) {
	return s.listActionPermissions(ctx, tokenID, now, true)
}

func (s *Store) listActionPermissions(ctx context.Context, tokenID int64, now time.Time, scoped bool) ([]ActionPermission, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("connector target store is not configured")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	scopeFilter := ""
	if scoped {
		scopeFilter = "AND scope.enabled = 1"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			p.token_id, t.project_id, project.name, project.slug, scope.enabled, p.target_id, t.name, p.profile_id, cp.label,
			t.connector_kind, cp.kind, p.action_name, p.execution_rule,
			COALESCE(p.expires_at, ''), p.created_at, p.updated_at
		FROM token_connector_action_permissions p
		JOIN connector_targets t ON t.id = p.target_id
		JOIN projects project ON project.id = t.project_id AND project.status = 'active'
		JOIN token_project_scopes scope ON scope.token_id = p.token_id AND scope.project_id = t.project_id
		JOIN connector_credential_profiles cp ON cp.id = p.profile_id AND cp.target_id = p.target_id
			WHERE
				p.token_id = ?
				AND t.status = 'active'
				AND cp.status = 'active'
				AND cp.connector_kind = t.connector_kind
				AND (COALESCE(p.expires_at, '') = '' OR p.expires_at > ?)
				`+scopeFilter+`
		ORDER BY t.connector_kind, t.name, cp.label, p.action_name`,
		tokenID,
		now.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("list connector action permissions: %w", err)
	}
	defer rows.Close()

	permissions := []ActionPermission{}
	for rows.Next() {
		item, err := scanActionPermission(rows)
		if err != nil {
			return nil, err
		}
		permissions = append(permissions, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connector action permissions: %w", err)
	}
	return permissions, nil
}

func (s *Store) InsertActionRequest(ctx context.Context, input InsertActionRequestInput) (ActionRequest, error) {
	request, _, err := s.InsertActionRequestIdempotent(ctx, input)
	return request, err
}

func (s *Store) InsertActionRequestIdempotent(ctx context.Context, input InsertActionRequestInput) (ActionRequest, bool, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, false, fmt.Errorf("connector target store is not configured")
	}
	if err := validateActionRequestInput(input); err != nil {
		return ActionRequest{}, false, err
	}
	inputJSON, err := jsonObjectString(input.Input)
	if err != nil {
		return ActionRequest{}, false, ValidationError("action input must be a JSON object")
	}
	previewJSON, err := jsonObjectString(input.Preview)
	if err != nil {
		return ActionRequest{}, false, ValidationError("action preview must be a JSON object")
	}
	now := nowString()
	idempotencyScope := actionRequestIdempotencyScope(input.TokenID, input.Source)
	executor, commit, rollback, err := s.transaction(ctx, "connector action request")
	if err != nil {
		return ActionRequest{}, false, err
	}
	defer rollback()
	result, err := executor.ExecContext(ctx, `
		INSERT INTO connector_action_requests (
			token_id, target_id, profile_id, connector_kind, action_name, title, summary,
			preview_json, source, input_json,
			encrypted_payload_json, reason, status, approval_context,
			approval_context_hash, idempotency_key, idempotency_identity_hash, idempotency_scope, created_at
		)
		SELECT ?, t.id, p.id, t.connector_kind, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		FROM connector_targets t
		JOIN connector_credential_profiles p ON p.target_id = t.id
		WHERE
			t.id = ?
				AND p.id = ?
				AND t.connector_kind = ?
				AND p.connector_kind = t.connector_kind
				AND t.status = 'active'
				AND p.status = 'active'
		ON CONFLICT DO NOTHING`,
		nullableInt64(input.TokenID),
		input.ActionName,
		strings.TrimSpace(input.Title),
		strings.TrimSpace(input.Summary),
		previewJSON,
		actionRequestSource(input.Source),
		inputJSON,
		strings.TrimSpace(input.EncryptedPayloadJSON),
		strings.TrimSpace(input.Reason),
		string(input.Status),
		strings.TrimSpace(input.ApprovalContext),
		strings.TrimSpace(input.ApprovalContextHash),
		strings.TrimSpace(input.IdempotencyKey),
		strings.TrimSpace(input.IdempotencyIdentityHash),
		idempotencyScope,
		now,
		input.TargetID,
		input.ProfileID,
		input.ConnectorKind,
	)
	if err != nil {
		return ActionRequest{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ActionRequest{}, false, err
	}
	if affected == 0 {
		if strings.TrimSpace(input.IdempotencyKey) != "" {
			existing, findErr := getActionRequestByIdempotencyWithExecutor(ctx, executor, idempotencyScope, input.IdempotencyKey)
			if findErr == nil {
				if existing.IdempotencyIdentityHash != strings.TrimSpace(input.IdempotencyIdentityHash) {
					return ActionRequest{}, false, ErrActionRequestIdempotency
				}
				if commitErr := commit(); commitErr != nil {
					return ActionRequest{}, false, commitErr
				}
				return existing, false, nil
			}
			if !errors.Is(findErr, ErrActionRequestNotFound) {
				return ActionRequest{}, false, findErr
			}
		}
		return ActionRequest{}, false, ErrTargetProfileNotFound
	}
	id, err := result.LastInsertId()
	if err != nil {
		return ActionRequest{}, false, err
	}
	request, err := getActionRequestWithExecutor(ctx, executor, id)
	if err != nil {
		return ActionRequest{}, false, err
	}
	if err := history.SyncConnectorActionRequestWithExecutor(ctx, executor, id); err != nil {
		return ActionRequest{}, false, err
	}
	if err := commit(); err != nil {
		return ActionRequest{}, false, err
	}
	return request, true, nil
}

func getActionRequestByIdempotencyWithExecutor(ctx context.Context, executor storeDB, scope, key string) (ActionRequest, error) {
	request, err := scanActionRequest(executor.QueryRowContext(ctx,
		actionRequestSelectSQL()+` WHERE r.idempotency_scope = ? AND r.idempotency_key = ?`,
		scope, strings.TrimSpace(key),
	))
	if errors.Is(err, sql.ErrNoRows) {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	return request, err
}

func actionRequestIdempotencyScope(tokenID *int64, source string) string {
	if tokenID != nil {
		return fmt.Sprintf("token:%d", *tokenID)
	}
	return "source:" + actionRequestSource(source)
}

func (s *Store) FinishActionRequest(ctx context.Context, input FinishActionRequestInput) (ActionRequest, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, fmt.Errorf("connector target store is not configured")
	}
	if input.ID < 1 {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	if !validActionRequestTerminalStatus(input.Status) {
		return ActionRequest{}, ValidationError("invalid terminal action request status")
	}
	outputJSON, err := jsonValueString(input.Output)
	if err != nil {
		return ActionRequest{}, ValidationError("action output must be valid JSON")
	}
	allowedStatuses, err := finishAllowedStatuses(input.AllowedStatuses)
	if err != nil {
		return ActionRequest{}, err
	}
	statusPlaceholders := strings.TrimRight(strings.Repeat("?,", len(allowedStatuses)), ",")
	now := nowString()
	args := []any{
		string(input.Status),
		outputJSON,
		strings.TrimSpace(input.DisplayText),
		strings.TrimSpace(input.Error),
		strings.TrimSpace(input.ApprovalDrift),
		now,
		input.ID,
	}
	for _, status := range allowedStatuses {
		args = append(args, string(status))
	}
	request, affected, err := s.mutateActionRequestAndSync(ctx, input.ID, func(executor storeDB) (sql.Result, error) {
		return executor.ExecContext(ctx, `
			UPDATE connector_action_requests
			SET status = ?, output_json = ?, display_text = ?, error = ?, approval_context_drift = ?, completed_at = ?
			WHERE id = ? AND status IN (`+statusPlaceholders+`)`,
			args...,
		)
	})
	if err != nil {
		return ActionRequest{}, err
	}
	if affected == 0 {
		return s.GetActionRequest(ctx, input.ID)
	}
	return request, nil
}

func (s *Store) SetActionRequestSessionHandle(ctx context.Context, id int64, sessionID int64, generation int64) (ActionRequest, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, fmt.Errorf("connector target store is not configured")
	}
	if id < 1 || sessionID < 1 || generation < 1 {
		return ActionRequest{}, ValidationError("an exact session handle is required")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE connector_action_requests
		SET session_id = ?, session_generation = ?
		WHERE id = ? AND status = ?`,
		sessionID, generation, id, string(connectors.ResultRunning),
	)
	if err != nil {
		return ActionRequest{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ActionRequest{}, err
	}
	if affected == 0 {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	return s.GetActionRequest(ctx, id)
}

func (s *Store) StaleActionRequestsForTarget(ctx context.Context, input StaleActionRequestsForTargetInput) (StaleActionRequestsForTargetResult, error) {
	if s == nil || s.db == nil {
		return StaleActionRequestsForTargetResult{}, fmt.Errorf("connector target store is not configured")
	}
	if input.TargetID < 1 {
		return StaleActionRequestsForTargetResult{}, ErrTargetNotFound
	}
	if input.ProfileID < 0 {
		return StaleActionRequestsForTargetResult{}, ErrTargetProfileNotFound
	}
	where := "target_id = ? AND status IN (?)"
	args := []any{input.TargetID, string(connectors.ResultApprovalPending)}
	if input.IncludeRunning {
		where = "target_id = ? AND status IN (?, ?)"
		args = []any{input.TargetID, string(connectors.ResultApprovalPending), string(connectors.ResultRunning)}
	}
	if input.ProfileID > 0 {
		where += " AND profile_id = ?"
		args = append(args, input.ProfileID)
	}
	executor, commit, rollback, err := s.transaction(ctx, "stale connector action requests")
	if err != nil {
		return StaleActionRequestsForTargetResult{}, err
	}
	defer rollback()
	rows, err := executor.QueryContext(ctx, `SELECT id FROM connector_action_requests WHERE `+where, args...)
	if err != nil {
		return StaleActionRequestsForTargetResult{}, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return StaleActionRequestsForTargetResult{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return StaleActionRequestsForTargetResult{}, err
	}
	if len(ids) == 0 {
		return StaleActionRequestsForTargetResult{}, nil
	}
	updateArgs := []any{string(connectors.ResultStale), strings.TrimSpace(input.Error), strings.TrimSpace(input.ApprovalDrift), nowString()}
	updateArgs = append(updateArgs, args...)
	result, err := executor.ExecContext(ctx, `
		UPDATE connector_action_requests
		SET status = ?, error = ?, approval_context_drift = ?, completed_at = COALESCE(completed_at, ?)
		WHERE `+where,
		updateArgs...,
	)
	if err != nil {
		return StaleActionRequestsForTargetResult{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return StaleActionRequestsForTargetResult{}, err
	}
	for _, id := range ids {
		if err := history.SyncConnectorActionRequestWithExecutor(ctx, executor, id); err != nil {
			return StaleActionRequestsForTargetResult{}, err
		}
	}
	if err := commit(); err != nil {
		return StaleActionRequestsForTargetResult{}, err
	}
	return StaleActionRequestsForTargetResult{IDs: ids, Affected: affected}, nil
}

func (s *Store) MarkActionRequestRunning(ctx context.Context, id int64) (ActionRequest, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, fmt.Errorf("connector target store is not configured")
	}
	if id < 1 {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	request, affected, err := s.mutateActionRequestAndSync(ctx, id, func(executor storeDB) (sql.Result, error) {
		return executor.ExecContext(ctx, `
			UPDATE connector_action_requests
			SET status = ?, error = ''
			WHERE id = ? AND status = ?`,
			string(connectors.ResultRunning),
			id,
			string(connectors.ResultApprovalPending),
		)
	})
	if err != nil {
		return ActionRequest{}, err
	}
	if affected == 0 {
		return ActionRequest{}, ErrActionRequestNotPending
	}
	return request, nil
}

func (s *Store) DeclineActionRequest(ctx context.Context, id int64, message string) (ActionRequest, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, fmt.Errorf("connector target store is not configured")
	}
	if id < 1 {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	now := nowString()
	request, affected, err := s.mutateActionRequestAndSync(ctx, id, func(executor storeDB) (sql.Result, error) {
		return executor.ExecContext(ctx, `
			UPDATE connector_action_requests
			SET status = ?, error = ?, completed_at = ?
			WHERE id = ? AND status = ?`,
			string(connectors.ResultDeclined),
			strings.TrimSpace(message),
			now,
			id,
			string(connectors.ResultApprovalPending),
		)
	})
	if err != nil {
		return ActionRequest{}, err
	}
	if affected == 0 {
		return ActionRequest{}, ErrActionRequestNotPending
	}
	return request, nil
}

func (s *Store) mutateActionRequestAndSync(ctx context.Context, id int64, mutate func(storeDB) (sql.Result, error)) (ActionRequest, int64, error) {
	executor, commit, rollback, err := s.transaction(ctx, "connector action request mutation")
	if err != nil {
		return ActionRequest{}, 0, err
	}
	defer rollback()
	result, err := mutate(executor)
	if err != nil {
		return ActionRequest{}, 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ActionRequest{}, 0, err
	}
	if affected != 1 {
		return ActionRequest{}, affected, nil
	}
	request, err := getActionRequestWithExecutor(ctx, executor, id)
	if err != nil {
		return ActionRequest{}, 0, err
	}
	if err := history.SyncConnectorActionRequestWithExecutor(ctx, executor, id); err != nil {
		return ActionRequest{}, 0, err
	}
	if err := commit(); err != nil {
		return ActionRequest{}, 0, err
	}
	return request, affected, nil
}

func getActionRequestWithExecutor(ctx context.Context, executor storeDB, id int64) (ActionRequest, error) {
	request, err := scanActionRequest(executor.QueryRowContext(ctx, actionRequestSelectSQL()+` WHERE r.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	if err != nil {
		return ActionRequest{}, err
	}
	return request, nil
}

func (s *Store) GetActionRequest(ctx context.Context, id int64) (ActionRequest, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, fmt.Errorf("connector target store is not configured")
	}
	if id < 1 {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	row := s.db.QueryRowContext(ctx, actionRequestSelectSQL()+` WHERE r.id = ?`, id)
	request, err := scanActionRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	if err != nil {
		return ActionRequest{}, err
	}
	return request, nil
}

func (s *Store) ListActionRequests(ctx context.Context, filter ActionRequestFilter) ([]ActionRequest, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("connector target store is not configured")
	}
	where := []string{"(? = '' OR r.status = ?)"}
	args := []any{strings.TrimSpace(filter.Status), strings.TrimSpace(filter.Status)}
	limit := filter.Limit
	if limit < 1 || limit > 100 {
		limit = 100
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, actionRequestSelectSQL()+`
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list connector action requests: %w", err)
	}
	defer rows.Close()
	items := []ActionRequest{}
	for rows.Next() {
		item, err := scanActionRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connector action requests: %w", err)
	}
	return items, nil
}

func (s *Store) validateActionPermissions(ctx context.Context, tokenID int64, inputs []SetActionPermissionInput) error {
	seen := map[string]bool{}
	for _, input := range inputs {
		input.TokenID = tokenID
		if err := validateActionPermissionInput(input); err != nil {
			return err
		}
		key := fmt.Sprintf("%d:%d:%s", input.TargetID, input.ProfileID, input.ActionName)
		if seen[key] {
			return ValidationError("connector action permissions must be unique per target, profile, and action")
		}
		seen[key] = true
		var exists int
		err := s.db.QueryRowContext(ctx, activeTargetProfileSQL(), input.TargetID, input.ProfileID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return ValidationError("connector target/profile does not exist")
		}
		if err != nil {
			return fmt.Errorf("validate connector target/profile: %w", err)
		}
	}
	return nil
}

func (s *Store) requireActiveTargetProfile(ctx context.Context, targetID int64, profileID int64) error {
	var exists int
	err := s.db.QueryRowContext(ctx, activeTargetProfileSQL(), targetID, profileID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTargetProfileNotFound
	}
	if err != nil {
		return fmt.Errorf("validate connector target/profile: %w", err)
	}
	return nil
}

func activeTargetProfileSQL() string {
	return `
		SELECT 1
		FROM connector_targets t
		JOIN connector_credential_profiles p ON p.target_id = t.id
		WHERE t.id = ? AND p.id = ? AND p.connector_kind = t.connector_kind AND t.status = 'active' AND p.status = 'active'`
}

func validateActionPermissionInput(input SetActionPermissionInput) error {
	if input.TokenID < 1 || input.TargetID < 1 || input.ProfileID < 1 {
		return ValidationError("token_id, target_id, and profile_id are required")
	}
	if !connectors.ValidIdentifier(input.ActionName) {
		return ValidationError("invalid action name")
	}
	switch input.ExecutionRule {
	case ActionPermissionAlwaysRun, ActionPermissionApprovalRequired, ActionPermissionBlocked:
	default:
		return ValidationError("invalid execution rule")
	}
	if input.ExecutionRule == ActionPermissionBlocked && input.ExpiresAt != nil {
		return ValidationError("expires_at is not supported for blocked permissions")
	}
	return nil
}

func actionPermissionExpiresAt(input SetActionPermissionInput) any {
	value := actionPermissionExpiresAtValue(input)
	if value == "" {
		return nil
	}
	return value
}

func actionPermissionExpiresAtValue(input SetActionPermissionInput) string {
	if input.ExpiresAt == nil {
		return ""
	}
	return input.ExpiresAt.UTC().Format(time.RFC3339)
}
