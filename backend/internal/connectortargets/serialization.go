package connectortargets

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

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

var (
	ErrInvalidTargetRef            = errors.New("invalid connector target ref")
	ErrTargetNotFound              = errors.New("connector target not found")
	ErrTargetProfileNotFound       = errors.New("connector target profile not found")
	ErrRuntimeSurfaceNotFound      = errors.New("connector runtime surface not found")
	ErrActionPermissionNotFound    = errors.New("connector action permission not found")
	ErrActionRequestNotFound       = errors.New("connector action request not found")
	ErrActionRequestNotPending     = errors.New("connector action request is not pending")
	ErrActionRequestIdempotency    = errors.New("connector action idempotency key was already used for a different request")
	ErrActionRequestInsertConflict = errors.New("connector action request could not be inserted; retry with the same idempotency key")
)

const MaxIdempotencyKeyBytes = 128

func jsonObjectString(value map[string]any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if !json.Valid(encoded) || len(encoded) == 0 || encoded[0] != '{' {
		return "", fmt.Errorf("value must be a JSON object")
	}
	return string(encoded), nil
}

func jsonValueString(value any) (string, error) {
	if value == nil {
		return "null", nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if !json.Valid(encoded) {
		return "", fmt.Errorf("value must be valid JSON")
	}
	return string(encoded), nil
}

func secretRevision(encryptedSecretJSON string) string {
	if strings.TrimSpace(encryptedSecretJSON) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(encryptedSecretJSON))
	return hex.EncodeToString(sum[:])
}

func parseJSONObject(value string) (map[string]any, error) {
	if strings.TrimSpace(value) == "" {
		return map[string]any{}, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return nil, err
	}
	if decoded == nil {
		return map[string]any{}, nil
	}
	return decoded, nil
}

func parseJSONValue(value string) (any, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTarget(row rowScanner) (Target, error) {
	var configJSON string
	var target Target
	if err := row.Scan(
		&target.ID,
		&target.ProjectID,
		&target.ProjectName,
		&target.ProjectSlug,
		&target.ConnectorKind,
		&target.Name,
		&configJSON,
		&target.Status,
		&target.CreatedAt,
		&target.UpdatedAt,
	); err != nil {
		return Target{}, err
	}
	config, err := parseJSONObject(configJSON)
	if err != nil {
		return Target{}, fmt.Errorf("decode connector target config: %w", err)
	}
	target.Config = config
	return target, nil
}

func (s *Store) resolveProjectID(ctx context.Context, projectID int64) (int64, error) {
	if projectID < 0 {
		return 0, ValidationError("invalid project id")
	}
	if projectID == 0 {
		err := s.db.QueryRowContext(ctx, `SELECT id FROM projects WHERE slug = 'ungrouped' AND status = 'active'`).Scan(&projectID)
		if err != nil {
			return 0, fmt.Errorf("resolve ungrouped project: %w", err)
		}
		return projectID, nil
	}
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ? AND status = 'active'`, projectID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ValidationError("project not found")
	}
	if err != nil {
		return 0, fmt.Errorf("validate project: %w", err)
	}
	return projectID, nil
}

func scanCredentialProfile(row rowScanner) (CredentialProfile, error) {
	var publicJSON string
	var profile CredentialProfile
	if err := row.Scan(
		&profile.ID,
		&profile.TargetID,
		&profile.ConnectorKind,
		&profile.Kind,
		&profile.Label,
		&publicJSON,
		&profile.EncryptedSecretJSON,
		&profile.RiskLabel,
		&profile.SecretRevision,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	); err != nil {
		return CredentialProfile{}, err
	}
	public, err := parseJSONObject(publicJSON)
	if err != nil {
		return CredentialProfile{}, fmt.Errorf("decode connector profile public metadata: %w", err)
	}
	profile.Public = public
	return profile, nil
}

func scanActionPermission(row rowScanner) (ActionPermission, error) {
	var item ActionPermission
	var projectEnabled int
	if err := row.Scan(
		&item.TokenID,
		&item.ProjectID,
		&item.ProjectName,
		&item.ProjectSlug,
		&projectEnabled,
		&item.TargetID,
		&item.TargetName,
		&item.ProfileID,
		&item.ProfileLabel,
		&item.ConnectorKind,
		&item.ProfileKind,
		&item.ActionName,
		&item.ExecutionRule,
		&item.ExpiresAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return ActionPermission{}, fmt.Errorf("scan connector action permission: %w", err)
	}
	item.ProjectEnabled = projectEnabled == 1
	return item, nil
}

func actionRequestSelectSQL() string {
	return `
		SELECT
			r.id, r.token_id, COALESCE(tok.name, ''),
			r.target_id, t.name, r.profile_id, p.label,
			r.connector_kind, r.action_name, r.title, r.summary, r.preview_json,
			r.source, r.input_json, r.encrypted_payload_json,
			r.reason, r.status, r.output_json, r.display_text, r.error,
			r.approval_context, r.approval_context_hash, r.approval_context_drift,
			r.retry_policy_json,
			r.idempotency_key, r.idempotency_identity_hash,
			r.session_id, r.session_generation,
			r.created_at, r.completed_at
		FROM connector_action_requests r
		JOIN connector_targets t ON t.id = r.target_id
		JOIN connector_credential_profiles p ON p.id = r.profile_id AND p.target_id = r.target_id AND p.connector_kind = r.connector_kind
		LEFT JOIN api_tokens tok ON tok.id = r.token_id`
}

func scanActionRequest(row rowScanner) (ActionRequest, error) {
	var request ActionRequest
	var tokenID sql.NullInt64
	var inputJSON string
	var previewJSON string
	var outputJSON string
	var retryPolicyJSON string
	var completedAt sql.NullString
	var sessionID sql.NullInt64
	var sessionGeneration sql.NullInt64
	if err := row.Scan(
		&request.ID,
		&tokenID,
		&request.TokenName,
		&request.TargetID,
		&request.TargetName,
		&request.ProfileID,
		&request.ProfileLabel,
		&request.ConnectorKind,
		&request.ActionName,
		&request.Title,
		&request.Summary,
		&previewJSON,
		&request.Source,
		&inputJSON,
		&request.EncryptedPayloadJSON,
		&request.Reason,
		&request.Status,
		&outputJSON,
		&request.DisplayText,
		&request.Error,
		&request.ApprovalContext,
		&request.ApprovalContextHash,
		&request.ApprovalContextDrift,
		&retryPolicyJSON,
		&request.IdempotencyKey,
		&request.IdempotencyIdentityHash,
		&sessionID,
		&sessionGeneration,
		&request.CreatedAt,
		&completedAt,
	); err != nil {
		return ActionRequest{}, fmt.Errorf("scan connector action request: %w", err)
	}
	if tokenID.Valid {
		request.TokenID = &tokenID.Int64
	}
	if sessionID.Valid {
		request.SessionID = &sessionID.Int64
	}
	if sessionGeneration.Valid {
		request.SessionGeneration = &sessionGeneration.Int64
	}
	input, err := parseJSONObject(inputJSON)
	if err != nil {
		return ActionRequest{}, fmt.Errorf("decode connector action input: %w", err)
	}
	request.Input = input
	preview, err := parseJSONObject(previewJSON)
	if err != nil {
		return ActionRequest{}, fmt.Errorf("decode connector action preview: %w", err)
	}
	request.Preview = preview
	output, err := parseJSONValue(outputJSON)
	if err != nil {
		return ActionRequest{}, fmt.Errorf("decode connector action output: %w", err)
	}
	request.Output = output
	if err := json.Unmarshal([]byte(retryPolicyJSON), &request.RetryPolicy); err != nil {
		return ActionRequest{}, fmt.Errorf("decode connector action retry policy: %w", err)
	}
	request.RetryPolicy = connectors.NormalizePersistedRetryPolicy(request.RetryPolicy)
	if completedAt.Valid {
		request.CompletedAt = &completedAt.String
	}
	return request, nil
}

func validateActionRequestInput(input InsertActionRequestInput) error {
	if input.TargetID < 1 || input.ProfileID < 1 {
		return ValidationError("target_id and profile_id are required")
	}
	if input.TokenID != nil && *input.TokenID < 1 {
		return ValidationError("token_id must be positive")
	}
	if !connectors.ValidIdentifier(input.ConnectorKind) {
		return ValidationError("invalid connector kind")
	}
	if !connectors.ValidIdentifier(input.ActionName) {
		return ValidationError("invalid action name")
	}
	if !validActionRequestStatus(input.Status) {
		return ValidationError("invalid action request status")
	}
	if len(strings.TrimSpace(input.IdempotencyKey)) > MaxIdempotencyKeyBytes {
		return ValidationError("idempotency_key is too long")
	}
	if strings.TrimSpace(input.IdempotencyKey) != "" && strings.TrimSpace(input.IdempotencyIdentityHash) == "" {
		return ValidationError("idempotency identity hash is required")
	}
	return nil
}

func actionRequestSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "mcp"
	}
	return source
}

func validActionRequestStatus(status connectors.ResultStatus) bool {
	switch status {
	case connectors.ResultCompleted,
		connectors.ResultFailed,
		connectors.ResultCanceled,
		connectors.ResultRunning,
		connectors.ResultApprovalPending,
		connectors.ResultBlocked,
		connectors.ResultStale,
		connectors.ResultDeclined,
		connectors.ResultError,
		connectors.ResultOutcomeUnknown:
		return true
	default:
		return false
	}
}

const actionRequestPreparingStatus connectors.ResultStatus = "preparing"

func validActionRequestTerminalStatus(status connectors.ResultStatus) bool {
	switch status {
	case connectors.ResultCompleted,
		connectors.ResultFailed,
		connectors.ResultCanceled,
		connectors.ResultBlocked,
		connectors.ResultStale,
		connectors.ResultDeclined,
		connectors.ResultError,
		connectors.ResultOutcomeUnknown:
		return true
	default:
		return false
	}
}

func finishAllowedStatuses(statuses []connectors.ResultStatus) ([]connectors.ResultStatus, error) {
	if len(statuses) == 0 {
		return []connectors.ResultStatus{connectors.ResultRunning}, nil
	}
	allowed := make([]connectors.ResultStatus, 0, len(statuses))
	seen := map[connectors.ResultStatus]bool{}
	for _, status := range statuses {
		switch status {
		case connectors.ResultRunning, connectors.ResultApprovalPending, connectors.ResultBlocked:
			if !seen[status] {
				allowed = append(allowed, status)
				seen[status] = true
			}
		default:
			return nil, ValidationError("invalid allowed action request status")
		}
	}
	if len(allowed) == 0 {
		return nil, ValidationError("allowed action request statuses are required")
	}
	return allowed, nil
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "constraint failed")
}
