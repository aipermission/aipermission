package connectortargets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/expirypolicy"
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
	RetryPolicy             connectors.RetryPolicy
	IdempotencyKey          string
	IdempotencyIdentityHash string
	SessionID               *int64
	SessionGeneration       *int64
	ExecutionOwner          string
	ExecutionLeaseExpiresAt string
	DispatchStartedAt       string
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
	RetryPolicy             connectors.RetryPolicy
	IdempotencyKey          string
	IdempotencyIdentityHash string
	ExecutionOwner          string
	ExecutionLeaseExpiresAt string
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

type InvalidateActionRequestsForTargetInput struct {
	TargetID       int64
	ProfileID      int64
	Error          string
	RunningError   string
	ApprovalDrift  string
	IncludeRunning bool
}

type InvalidateActionRequestsForTargetResult struct {
	IDs               []int64
	StaleIDs          []int64
	OutcomeUnknownIDs []int64
	Affected          int64
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
				AND cp.connector_kind = t.connector_kind`,
		tokenID,
		targetID,
		profileID,
		actionName,
	)
	permission, err := scanActionPermission(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ActionPermission{}, ErrActionPermissionNotFound
	}
	if err != nil {
		return ActionPermission{}, err
	}
	if !expirypolicy.Active(permission.ExpiresAt, now) {
		return ActionPermission{}, ErrActionPermissionNotFound
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
			`+scopeFilter+`
		ORDER BY t.connector_kind, t.name, cp.label, p.action_name`,
		tokenID,
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
		if !expirypolicy.Active(item.ExpiresAt, now) {
			continue
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
	if input.Status == connectors.ResultApprovalPending && !hasActionRequestApprovalIntegrity(input) {
		return ActionRequest{}, false, ValidationError("pending connector action approval integrity data is required")
	}
	return s.insertActionRequestIdempotent(ctx, input, nil)
}

// InsertSealedActionRequestIdempotent reserves the record ID, seals the
// execution payload against that ID, and publishes the final lifecycle state
// in one transaction. The temporary state is never externally observable.
func (s *Store) InsertSealedActionRequestIdempotent(
	ctx context.Context,
	input InsertActionRequestInput,
	seal func(int64) (string, error),
) (ActionRequest, bool, error) {
	if seal == nil {
		return ActionRequest{}, false, ValidationError("action payload sealer is required")
	}
	if input.Status == connectors.ResultApprovalPending &&
		(strings.TrimSpace(input.ApprovalContext) == "" || strings.TrimSpace(input.ApprovalContextHash) == "") {
		return ActionRequest{}, false, ValidationError("pending connector action approval context is required")
	}
	return s.insertActionRequestIdempotent(ctx, input, seal)
}

func hasActionRequestApprovalIntegrity(input InsertActionRequestInput) bool {
	return strings.TrimSpace(input.EncryptedPayloadJSON) != "" &&
		strings.TrimSpace(input.ApprovalContext) != "" &&
		strings.TrimSpace(input.ApprovalContextHash) != ""
}

func (s *Store) insertActionRequestIdempotent(ctx context.Context, input InsertActionRequestInput, seal func(int64) (string, error)) (ActionRequest, bool, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, false, fmt.Errorf("connector target store is not configured")
	}
	if err := validateActionRequestInput(input); err != nil {
		return ActionRequest{}, false, err
	}
	finalStatus := input.Status
	if seal != nil {
		input.Status = actionRequestPreparingStatus
	}
	inputJSON, err := jsonObjectString(input.Input)
	if err != nil {
		return ActionRequest{}, false, ValidationError("action input must be a JSON object")
	}
	previewJSON, err := jsonObjectString(input.Preview)
	if err != nil {
		return ActionRequest{}, false, ValidationError("action preview must be a JSON object")
	}
	retryPolicyJSON, err := json.Marshal(input.RetryPolicy)
	if err != nil {
		return ActionRequest{}, false, ValidationError("retry policy must be JSON serializable")
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
			approval_context_hash, retry_policy_json, idempotency_key, idempotency_identity_hash, idempotency_scope,
			execution_owner, execution_lease_expires_at, created_at
		)
		SELECT ?, t.id, p.id, t.connector_kind, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
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
		string(retryPolicyJSON),
		strings.TrimSpace(input.IdempotencyKey),
		strings.TrimSpace(input.IdempotencyIdentityHash),
		idempotencyScope,
		strings.TrimSpace(input.ExecutionOwner),
		strings.TrimSpace(input.ExecutionLeaseExpiresAt),
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
			var activePair int
			if pairErr := executor.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM connector_targets t
				JOIN connector_credential_profiles p ON p.target_id = t.id
				WHERE t.id = ? AND p.id = ? AND t.connector_kind = ?
					AND p.connector_kind = t.connector_kind
					AND t.status = 'active' AND p.status = 'active'`,
				input.TargetID, input.ProfileID, input.ConnectorKind,
			).Scan(&activePair); pairErr != nil {
				return ActionRequest{}, false, pairErr
			}
			if activePair > 0 {
				return ActionRequest{}, false, ErrActionRequestInsertConflict
			}
		}
		return ActionRequest{}, false, ErrTargetProfileNotFound
	}
	id, err := result.LastInsertId()
	if err != nil {
		return ActionRequest{}, false, err
	}
	if seal != nil {
		encryptedPayload, sealErr := seal(id)
		if sealErr != nil {
			return ActionRequest{}, false, sealErr
		}
		if strings.TrimSpace(encryptedPayload) == "" {
			return ActionRequest{}, false, ValidationError("sealed action payload is required")
		}
		finalized, finalizeErr := executor.ExecContext(ctx, `
			UPDATE connector_action_requests
			SET encrypted_payload_json = ?, status = ?
			WHERE id = ? AND status = ?`,
			encryptedPayload, string(finalStatus), id, string(actionRequestPreparingStatus),
		)
		if finalizeErr != nil {
			return ActionRequest{}, false, fmt.Errorf("finalize sealed connector action request: %w", finalizeErr)
		}
		finalizedCount, finalizeErr := finalized.RowsAffected()
		if finalizeErr != nil {
			return ActionRequest{}, false, fmt.Errorf("read finalized connector action request rows affected: %w", finalizeErr)
		}
		if finalizedCount != 1 {
			return ActionRequest{}, false, ErrActionRequestInsertConflict
		}
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

func (s *Store) GetActionRequestByIdempotency(ctx context.Context, tokenID *int64, source, key string) (ActionRequest, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, fmt.Errorf("connector target store is not configured")
	}
	if strings.TrimSpace(key) == "" {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	return getActionRequestByIdempotencyWithExecutor(ctx, s.db, actionRequestIdempotencyScope(tokenID, source), key)
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
	request, _, err := s.FinishActionRequestWithChange(ctx, input)
	return request, err
}

// FinishActionRequestWithChange reports whether the terminal transition was
// applied. Lifecycle callers use the flag to avoid auditing late completions.
func (s *Store) FinishActionRequestWithChange(ctx context.Context, input FinishActionRequestInput) (ActionRequest, bool, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, false, fmt.Errorf("connector target store is not configured")
	}
	if input.ID < 1 {
		return ActionRequest{}, false, ErrActionRequestNotFound
	}
	if !validActionRequestTerminalStatus(input.Status) {
		return ActionRequest{}, false, ValidationError("invalid terminal action request status")
	}
	outputJSON, err := jsonValueString(input.Output)
	if err != nil {
		return ActionRequest{}, false, ValidationError("action output must be valid JSON")
	}
	allowedStatuses, err := finishAllowedStatuses(input.AllowedStatuses)
	if err != nil {
		return ActionRequest{}, false, err
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
		return ActionRequest{}, false, err
	}
	if affected == 0 {
		request, err := s.GetActionRequest(ctx, input.ID)
		return request, false, err
	}
	return request, true, nil
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

func (s *Store) InvalidateActionRequestsForTarget(ctx context.Context, input InvalidateActionRequestsForTargetInput) (InvalidateActionRequestsForTargetResult, error) {
	if s == nil || s.db == nil {
		return InvalidateActionRequestsForTargetResult{}, fmt.Errorf("connector target store is not configured")
	}
	if input.TargetID < 1 {
		return InvalidateActionRequestsForTargetResult{}, ErrTargetNotFound
	}
	if input.ProfileID < 0 {
		return InvalidateActionRequestsForTargetResult{}, ErrTargetProfileNotFound
	}
	where := "target_id = ?"
	args := []any{input.TargetID}
	statuses := []connectors.ResultStatus{connectors.ResultApprovalPending}
	if input.IncludeRunning {
		statuses = append(statuses, connectors.ResultRunning)
	}
	if input.ProfileID > 0 {
		where += " AND profile_id = ?"
		args = append(args, input.ProfileID)
	}
	executor, commit, rollback, err := s.transaction(ctx, "stale connector action requests")
	if err != nil {
		return InvalidateActionRequestsForTargetResult{}, err
	}
	defer rollback()
	statusValues := make([]any, 0, len(statuses))
	statusPlaceholders := make([]string, 0, len(statuses))
	for _, status := range statuses {
		statusValues = append(statusValues, string(status))
		statusPlaceholders = append(statusPlaceholders, "?")
	}
	queryArgs := append(append([]any{}, args...), statusValues...)
	rows, err := executor.QueryContext(ctx, `SELECT id, status FROM connector_action_requests WHERE `+where+` AND status IN (`+strings.Join(statusPlaceholders, ",")+`) ORDER BY id`, queryArgs...)
	if err != nil {
		return InvalidateActionRequestsForTargetResult{}, err
	}
	result := InvalidateActionRequestsForTargetResult{}
	for rows.Next() {
		var id int64
		var status connectors.ResultStatus
		if err := rows.Scan(&id, &status); err != nil {
			rows.Close()
			return InvalidateActionRequestsForTargetResult{}, err
		}
		result.IDs = append(result.IDs, id)
		if status == connectors.ResultRunning {
			result.OutcomeUnknownIDs = append(result.OutcomeUnknownIDs, id)
		} else {
			result.StaleIDs = append(result.StaleIDs, id)
		}
	}
	if err := rows.Close(); err != nil {
		return InvalidateActionRequestsForTargetResult{}, err
	}
	if len(result.IDs) == 0 {
		return InvalidateActionRequestsForTargetResult{}, nil
	}
	updateArgs := []any{string(connectors.ResultStale), strings.TrimSpace(input.Error), strings.TrimSpace(input.ApprovalDrift), nowString()}
	updateArgs = append(updateArgs, args...)
	updateArgs = append(updateArgs, string(connectors.ResultApprovalPending))
	updated, err := executor.ExecContext(ctx, `
		UPDATE connector_action_requests
		SET status = ?, error = ?, approval_context_drift = ?, completed_at = COALESCE(completed_at, ?)
		WHERE `+where+` AND status = ?`,
		updateArgs...,
	)
	if err != nil {
		return InvalidateActionRequestsForTargetResult{}, err
	}
	affected, err := updated.RowsAffected()
	if err != nil {
		return InvalidateActionRequestsForTargetResult{}, err
	}
	result.Affected = affected
	if input.IncludeRunning {
		runningError := strings.TrimSpace(input.RunningError)
		if runningError == "" {
			runningError = "connector target changed after dispatch; the external outcome is unknown"
		}
		runningArgs := []any{string(connectors.ResultOutcomeUnknown), runningError, strings.TrimSpace(input.ApprovalDrift), nowString()}
		runningArgs = append(runningArgs, args...)
		runningArgs = append(runningArgs, string(connectors.ResultRunning))
		updated, err := executor.ExecContext(ctx, `
			UPDATE connector_action_requests
			SET status = ?, error = ?, approval_context_drift = ?, completed_at = COALESCE(completed_at, ?)
			WHERE `+where+` AND status = ?`, runningArgs...)
		if err != nil {
			return InvalidateActionRequestsForTargetResult{}, err
		}
		runningAffected, err := updated.RowsAffected()
		if err != nil {
			return InvalidateActionRequestsForTargetResult{}, err
		}
		result.Affected += runningAffected
	}
	for _, id := range result.IDs {
		if err := history.SyncConnectorActionRequestWithExecutor(ctx, executor, id); err != nil {
			return InvalidateActionRequestsForTargetResult{}, err
		}
	}
	if err := commit(); err != nil {
		return InvalidateActionRequestsForTargetResult{}, err
	}
	return result, nil
}

func (s *Store) MarkActionRequestRunning(ctx context.Context, id int64, owner string, leaseExpiresAt time.Time) (ActionRequest, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, fmt.Errorf("connector target store is not configured")
	}
	owner = strings.TrimSpace(owner)
	if id < 1 || owner == "" || leaseExpiresAt.IsZero() {
		return ActionRequest{}, ErrActionRequestNotFound
	}
	request, affected, err := s.mutateActionRequestAndSync(ctx, id, func(executor storeDB) (sql.Result, error) {
		return executor.ExecContext(ctx, `
			UPDATE connector_action_requests
			SET status = ?, error = '', execution_owner = ?, execution_lease_expires_at = ?, dispatch_started_at = ''
			WHERE id = ? AND status = ?
				AND trim(encrypted_payload_json) <> ''
				AND trim(approval_context) <> ''
				AND trim(approval_context_hash) <> ''`,
			string(connectors.ResultRunning),
			owner,
			leaseExpiresAt.UTC().Format(time.RFC3339Nano),
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

func (s *Store) BeginActionRequestDispatch(ctx context.Context, id int64, owner string, now time.Time, leaseExpiresAt time.Time) (ActionRequest, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, fmt.Errorf("connector target store is not configured")
	}
	owner = strings.TrimSpace(owner)
	if id < 1 || owner == "" || now.IsZero() || !leaseExpiresAt.After(now) {
		return ActionRequest{}, ErrActionRequestExecutionClaim
	}
	request, affected, err := s.mutateActionRequestAndSync(ctx, id, func(executor storeDB) (sql.Result, error) {
		return executor.ExecContext(ctx, `
			UPDATE connector_action_requests
			SET dispatch_started_at = ?, execution_lease_expires_at = ?
			WHERE id = ? AND status = ? AND execution_owner = ? AND dispatch_started_at = ''
				AND execution_lease_expires_at <> ''
				AND julianday(execution_lease_expires_at) >= julianday(?)`,
			now.UTC().Format(time.RFC3339Nano),
			leaseExpiresAt.UTC().Format(time.RFC3339Nano),
			id,
			string(connectors.ResultRunning),
			owner,
			now.UTC().Format(time.RFC3339Nano),
		)
	})
	if err != nil {
		return ActionRequest{}, err
	}
	if affected != 1 {
		return ActionRequest{}, ErrActionRequestExecutionClaim
	}
	return request, nil
}

// RecoverExpiredActionRequest terminalizes a running request only while its
// durable execution lease is still missing or expired. The lease predicate is
// part of the update so a concurrent dispatch renewal cannot be overwritten.
func (s *Store) RecoverExpiredActionRequest(ctx context.Context, id int64, now time.Time, errorText string) (ActionRequest, bool, error) {
	if s == nil || s.db == nil {
		return ActionRequest{}, false, fmt.Errorf("connector target store is not configured")
	}
	if id < 1 || now.IsZero() {
		return ActionRequest{}, false, ErrActionRequestNotFound
	}
	request, affected, err := s.mutateActionRequestAndSync(ctx, id, func(executor storeDB) (sql.Result, error) {
		return executor.ExecContext(ctx, `
			UPDATE connector_action_requests
			SET status = ?, error = ?, completed_at = ?
			WHERE id = ? AND status = ?
			  AND (execution_lease_expires_at = '' OR julianday(execution_lease_expires_at) <= julianday(?))`,
			string(connectors.ResultOutcomeUnknown),
			strings.TrimSpace(errorText),
			now.UTC().Format(time.RFC3339Nano),
			id,
			string(connectors.ResultRunning),
			now.UTC().Format(time.RFC3339Nano),
		)
	})
	if err != nil {
		return ActionRequest{}, false, err
	}
	if affected == 0 {
		request, err := s.GetActionRequest(ctx, id)
		return request, false, err
	}
	return request, true, nil
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
