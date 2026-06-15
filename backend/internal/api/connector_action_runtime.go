package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

const (
	connectorActionToolName          = "connector.call_action"
	connectorActionApprovalHint      = "Wait 3 seconds, then poll this connector action request until it is completed, failed, declined, stale, or blocked."
	connectorActionRunningHint       = "Wait 3 seconds, then call get_connector_action_request again. Use the connector-specific read or recovery actions when the connector exposes them."
	connectorActionMissingPermission = "This token is not allowed to run this connector action for the selected target/profile"
)

type connectorActionCall struct {
	Source     string
	TokenID    int64
	TargetRef  string
	ActionName string
	Input      map[string]any
	Reason     string
}

type connectorActionCallResult struct {
	Request    connectortargets.ActionRequest
	Permission connectortargets.ActionPermission
	Result     connectors.ActionResult
}

type connectorActionExecutionEnvelope struct {
	Input   map[string]any `json:"input"`
	Payload map[string]any `json:"payload"`
	Reason  string         `json:"reason,omitempty"`
}

type connectorSecretAccessor struct {
	values map[string]any
}

func (a connectorSecretAccessor) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := a.values[name]
	if !ok || value == nil {
		return "", fmt.Errorf("connector secret %q not found", name)
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	default:
		return fmt.Sprint(typed), nil
	}
}

type noopConnectorEventSink struct{}

func (noopConnectorEventSink) Emit(context.Context, connectors.ActionEvent) error { return nil }

func (s *Server) callConnectorAction(ctx context.Context, runtime *databaseRuntime, call connectorActionCall) (connectorActionCallResult, error) {
	if runtime == nil || runtime.database == nil {
		return connectorActionCallResult{}, fmt.Errorf("database runtime is not available")
	}
	if call.TokenID < 1 {
		return connectorActionCallResult{}, fmt.Errorf("token_id is required")
	}
	if call.Source == "" {
		call.Source = commandRequestSourceMCP
	}
	prepared, err := runtime.prepareConnectorAction(ctx, actions.PrepareRequest{
		Source:     call.Source,
		TargetRef:  call.TargetRef,
		ActionName: call.ActionName,
		Input:      call.Input,
		Reason:     call.Reason,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		return connectorActionCallResult{}, err
	}

	store := connectortargets.NewStore(runtime.database)
	permission, err := store.GetActionPermission(ctx, call.TokenID, prepared.Target.ID, prepared.Profile.ID, prepared.Action.ActionName, time.Now().UTC())
	if errors.Is(err, connectortargets.ErrActionPermissionNotFound) {
		request, insertErr := s.insertConnectorActionRequest(ctx, runtime, call.TokenID, prepared, connectortargets.ActionPermission{}, connectors.ResultBlocked, connectorActionMissingPermission)
		if insertErr != nil {
			return connectorActionCallResult{}, insertErr
		}
		return connectorActionCallResult{
			Request: request,
			Result:  connectors.ActionResult{Status: connectors.ResultBlocked, Error: connectorActionMissingPermission},
		}, nil
	}
	if err != nil {
		return connectorActionCallResult{}, err
	}
	if permission.ExecutionRule == connectortargets.ActionPermissionBlocked {
		request, insertErr := s.insertConnectorActionRequest(ctx, runtime, call.TokenID, prepared, permission, connectors.ResultBlocked, "Connector action is blocked for this token")
		if insertErr != nil {
			return connectorActionCallResult{}, insertErr
		}
		return connectorActionCallResult{
			Request:    request,
			Permission: permission,
			Result:     connectors.ActionResult{Status: connectors.ResultBlocked, Error: "Connector action is blocked for this token"},
		}, nil
	}
	if permission.ExecutionRule == connectortargets.ActionPermissionApprovalRequired {
		request, insertErr := s.insertConnectorActionRequest(ctx, runtime, call.TokenID, prepared, permission, connectors.ResultApprovalPending, "")
		if insertErr != nil {
			return connectorActionCallResult{}, insertErr
		}
		return connectorActionCallResult{
			Request:    request,
			Permission: permission,
			Result: connectors.ActionResult{
				Status: connectors.ResultApprovalPending,
				Error:  "Waiting for user approval.",
				Handles: connectors.ActionHandles{
					RequestID:    request.ID,
					FollowupTool: "get_connector_action_request",
				},
			},
		}, nil
	}

	request, err := s.insertConnectorActionRequest(ctx, runtime, call.TokenID, prepared, permission, connectors.ResultRunning, "")
	if err != nil {
		return connectorActionCallResult{}, err
	}
	result, err := s.executePreparedConnectorAction(ctx, runtime, prepared)
	if err != nil {
		finished, finishErr := s.finishConnectorActionRequest(ctx, runtime, request.ID, connectors.ResultFailed, nil, "", err.Error(), prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return connectorActionCallResult{}, finishErr
		}
		return connectorActionCallResult{Request: finished, Permission: permission, Result: connectors.ActionResult{Status: connectors.ResultFailed, Error: finished.Error}}, nil
	}
	status := result.Status
	if status == "" {
		status = connectors.ResultCompleted
	}
	if status == connectors.ResultRunning {
		if !connectorActionSupportsRunning(prepared) {
			finished, finishErr := s.finishConnectorActionRequest(ctx, runtime, request.ID, connectors.ResultError, nil, "", "connector returned running for an action that does not support asynchronous execution", prepared.ActionDefinition.OutputHint)
			if finishErr != nil {
				return connectorActionCallResult{}, finishErr
			}
			return connectorActionCallResult{
				Request:    finished,
				Permission: permission,
				Result: connectors.ActionResult{
					Status: connectors.ResultError,
					Error:  "connector returned running for an action that does not support asynchronous execution",
				},
			}, nil
		}
		result.Handles.RequestID = request.ID
		if result.Handles.FollowupTool == "" {
			result.Handles.FollowupTool = "get_connector_action_request"
		}
		result = s.redactConnectorActionResult(context.Background(), runtime, result, prepared.ActionDefinition.OutputHint)
		go s.finishActiveConnectorActionRequest(runtime, request.ID, prepared)
		return connectorActionCallResult{Request: request, Permission: permission, Result: result}, nil
	}
	if status == connectors.ResultApprovalPending {
		status = connectors.ResultFailed
		result.Error = "connector returned approval_pending after execution was already allowed"
	}
	result = s.redactConnectorActionResult(context.Background(), runtime, result, prepared.ActionDefinition.OutputHint)
	finished, err := store.FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
		ID:          request.ID,
		Status:      status,
		Output:      result.Output,
		DisplayText: result.DisplayText,
		Error:       result.Error,
	})
	if err != nil {
		return connectorActionCallResult{}, err
	}
	if err := history.NewStore(runtime.database).SyncConnectorActionRequest(ctx, finished.ID); err != nil {
		return connectorActionCallResult{}, err
	}
	return connectorActionCallResult{Request: finished, Permission: permission, Result: result}, nil
}

func (s *Server) runLocalConnectorAction(ctx context.Context, runtime *databaseRuntime, call connectorActionCall) (connectorActionCallResult, error) {
	if runtime == nil || runtime.database == nil {
		return connectorActionCallResult{}, fmt.Errorf("database runtime is not available")
	}
	if call.Source == "" {
		call.Source = commandRequestSourceManual
	}
	prepared, err := runtime.prepareConnectorAction(ctx, actions.PrepareRequest{
		Source:     call.Source,
		TargetRef:  call.TargetRef,
		ActionName: call.ActionName,
		Input:      call.Input,
		Reason:     call.Reason,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		return connectorActionCallResult{}, err
	}

	request, err := s.insertPreparedConnectorActionRequest(ctx, runtime, nil, prepared, connectors.ResultRunning, "", "", "")
	if err != nil {
		return connectorActionCallResult{}, err
	}
	result, err := s.executePreparedConnectorAction(ctx, runtime, prepared)
	if err != nil {
		finished, finishErr := s.finishConnectorActionRequest(ctx, runtime, request.ID, connectors.ResultFailed, nil, "", err.Error(), prepared.ActionDefinition.OutputHint)
		if finishErr != nil {
			return connectorActionCallResult{}, finishErr
		}
		return connectorActionCallResult{Request: finished, Result: connectors.ActionResult{Status: connectors.ResultFailed, Error: finished.Error}}, nil
	}
	status := result.Status
	if status == "" {
		status = connectors.ResultCompleted
	}
	if status == connectors.ResultRunning {
		if !connectorActionSupportsRunning(prepared) {
			finished, finishErr := s.finishConnectorActionRequest(ctx, runtime, request.ID, connectors.ResultError, nil, "", "connector returned running for a local action that does not support asynchronous execution", prepared.ActionDefinition.OutputHint)
			if finishErr != nil {
				return connectorActionCallResult{}, finishErr
			}
			return connectorActionCallResult{
				Request: finished,
				Result: connectors.ActionResult{
					Status: connectors.ResultError,
					Error:  "connector returned running for a local action that does not support asynchronous execution",
				},
			}, nil
		}
		result.Handles.RequestID = request.ID
		result = s.redactConnectorActionResult(context.Background(), runtime, result, prepared.ActionDefinition.OutputHint)
		go s.finishActiveConnectorActionRequest(runtime, request.ID, prepared)
		return connectorActionCallResult{Request: request, Result: result}, nil
	}
	if status == connectors.ResultApprovalPending {
		status = connectors.ResultFailed
		result.Error = "connector returned approval_pending for a local operator action"
	}
	result = s.redactConnectorActionResult(context.Background(), runtime, result, prepared.ActionDefinition.OutputHint)
	finished, err := connectortargets.NewStore(runtime.database).FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
		ID:          request.ID,
		Status:      status,
		Output:      result.Output,
		DisplayText: result.DisplayText,
		Error:       result.Error,
	})
	if err != nil {
		return connectorActionCallResult{}, err
	}
	if err := history.NewStore(runtime.database).SyncConnectorActionRequest(ctx, finished.ID); err != nil {
		return connectorActionCallResult{}, err
	}
	return connectorActionCallResult{Request: finished, Result: result}, nil
}

func (s *Server) insertConnectorActionRequest(
	ctx context.Context,
	runtime *databaseRuntime,
	tokenID int64,
	prepared actions.PreparedRequest,
	permission connectortargets.ActionPermission,
	status connectors.ResultStatus,
	errorText string,
) (connectortargets.ActionRequest, error) {
	capturedAt := time.Now().UTC().Format(time.RFC3339)
	token, err := runtime.tokens.Get(ctx, tokenID)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	approvalContext, approvalHash, err := connectorApprovalContext(prepared, token, permission, capturedAt)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return s.insertPreparedConnectorActionRequest(ctx, runtime, &tokenID, prepared, status, errorText, approvalContext, approvalHash)
}

func (s *Server) insertPreparedConnectorActionRequest(
	ctx context.Context,
	runtime *databaseRuntime,
	tokenID *int64,
	prepared actions.PreparedRequest,
	status connectors.ResultStatus,
	errorText string,
	approvalContext string,
	approvalHash string,
) (connectortargets.ActionRequest, error) {
	payload, err := runtime.vault.EncryptJSON(connectorActionExecutionEnvelope{
		Input:   prepared.Requested.Input,
		Payload: prepared.Action.Payload,
		Reason:  prepared.Requested.Reason,
	})
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	request, err := connectortargets.NewStore(runtime.database).InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              tokenID,
		TargetID:             prepared.Target.ID,
		ProfileID:            prepared.Profile.ID,
		ConnectorKind:        prepared.Target.ConnectorKind,
		ActionName:           prepared.Action.ActionName,
		Title:                s.redactForPersistence(ctx, runtime, prepared.Action.Title),
		Summary:              s.redactForPersistence(ctx, runtime, prepared.Action.Summary),
		Preview:              s.redactConnectorActionPreview(ctx, runtime, prepared.Action.Preview, prepared.ActionDefinition.OutputHint),
		Source:               prepared.Requested.Source,
		Input:                s.redactConnectorActionInput(ctx, runtime, prepared.Requested.Input),
		EncryptedPayloadJSON: payload,
		Reason:               s.redactForPersistence(ctx, runtime, prepared.Requested.Reason),
		Status:               status,
		ApprovalContext:      approvalContext,
		ApprovalContextHash:  approvalHash,
	})
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	if err := history.NewStore(runtime.database).SyncConnectorActionRequest(ctx, request.ID); err != nil {
		return connectortargets.ActionRequest{}, err
	}
	if errorText != "" {
		if status == connectors.ResultBlocked {
			return s.finishConnectorActionRequestWithAllowed(ctx, runtime, request.ID, status, nil, "", errorText, []connectors.ResultStatus{connectors.ResultBlocked}, prepared.ActionDefinition.OutputHint)
		}
		return s.finishConnectorActionRequest(ctx, runtime, request.ID, status, nil, "", errorText, prepared.ActionDefinition.OutputHint)
	}
	return request, nil
}

func (s *Server) executePreparedConnectorAction(ctx context.Context, runtime *databaseRuntime, prepared actions.PreparedRequest) (connectors.ActionResult, error) {
	connector, ok := runtime.connectorRegistry().Get(prepared.Target.ConnectorKind)
	if !ok {
		return connectors.ActionResult{}, fmt.Errorf("connector not found: %s", prepared.Target.ConnectorKind)
	}
	profile, err := connectortargets.NewStore(runtime.database).GetCredentialProfile(ctx, prepared.Target.ID, prepared.Profile.ID)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	secrets := map[string]any{}
	if profile.EncryptedSecretJSON != "" {
		if err := runtime.vault.DecryptJSON(profile.EncryptedSecretJSON, &secrets); err != nil {
			return connectors.ActionResult{}, err
		}
	}
	return connector.ExecuteAction(ctx, connectors.RuntimeContext{
		Target:       prepared.Target,
		Profile:      prepared.Profile,
		Secrets:      connectorSecretAccessor{values: secrets},
		Events:       noopConnectorEventSink{},
		Capabilities: connectorRuntimeCapabilitiesFor(prepared.Target.ConnectorKind, s, runtime),
	}, prepared.Action)
}

func (s *Server) finishActiveConnectorActionRequest(runtime *databaseRuntime, requestID int64, prepared actions.PreparedRequest) {
	adapter := connectorRuntimeAdapterFor(prepared.Target.ConnectorKind)
	if adapter == nil || !adapter.SupportsRunning(prepared) {
		return
	}
	adapter.FinishRunning(s, runtime, requestID, prepared)
}

func connectorActionSupportsRunning(prepared actions.PreparedRequest) bool {
	adapter := connectorRuntimeAdapterFor(prepared.Target.ConnectorKind)
	return adapter != nil && adapter.SupportsRunning(prepared)
}

func (s *Server) finishConnectorActionRequest(ctx context.Context, runtime *databaseRuntime, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	return s.finishConnectorActionRequestWithAllowed(ctx, runtime, requestID, status, output, displayText, errorText, nil, hints...)
}

func (s *Server) finishConnectorActionRequestWithAllowed(ctx context.Context, runtime *databaseRuntime, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, allowedStatuses []connectors.ResultStatus, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	redacted := s.redactConnectorActionResult(ctx, runtime, connectors.ActionResult{
		Output:      output,
		DisplayText: displayText,
		Error:       errorText,
	}, hints...)
	finished, err := connectortargets.NewStore(runtime.database).FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
		ID:              requestID,
		Status:          status,
		Output:          redacted.Output,
		DisplayText:     redacted.DisplayText,
		Error:           redacted.Error,
		AllowedStatuses: allowedStatuses,
	})
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return finished, history.NewStore(runtime.database).SyncConnectorActionRequest(ctx, finished.ID)
}

func (s *Server) redactedConnectorValue(ctx context.Context, runtime *databaseRuntime, value any, sensitiveFields map[string]bool) any {
	switch typed := value.(type) {
	case string:
		return s.redactForPersistence(ctx, runtime, typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, s.redactedConnectorValue(ctx, runtime, item, sensitiveFields))
		}
		return out
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, s.redactForPersistence(ctx, runtime, item))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if connectorOutputFieldSensitive(key, sensitiveFields) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = s.redactedConnectorValue(ctx, runtime, item, sensitiveFields)
		}
		return out
	default:
		return value
	}
}

func (s *Server) redactConnectorActionResult(ctx context.Context, runtime *databaseRuntime, result connectors.ActionResult, hints ...connectors.OutputHint) connectors.ActionResult {
	sensitiveFields := connectorSensitiveOutputFields(hints...)
	result.DisplayText = s.redactForPersistence(ctx, runtime, result.DisplayText)
	result.Error = s.redactForPersistence(ctx, runtime, result.Error)
	result.Output = s.redactedConnectorValue(ctx, runtime, result.Output, sensitiveFields)
	return result
}

func (s *Server) redactConnectorActionInput(ctx context.Context, runtime *databaseRuntime, input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	redacted, ok := s.redactedConnectorValue(ctx, runtime, input, connectorSensitiveOutputFields()).(map[string]any)
	if !ok || redacted == nil {
		return map[string]any{}
	}
	return redacted
}

func (s *Server) redactConnectorActionPreview(ctx context.Context, runtime *databaseRuntime, preview map[string]any, hints ...connectors.OutputHint) map[string]any {
	if preview == nil {
		return map[string]any{}
	}
	redacted, ok := s.redactedConnectorValue(ctx, runtime, preview, connectorSensitiveOutputFields(hints...)).(map[string]any)
	if !ok || redacted == nil {
		return map[string]any{}
	}
	return redacted
}

func connectorSensitiveOutputFields(hints ...connectors.OutputHint) map[string]bool {
	fields := map[string]bool{
		"api_key":          true,
		"api_token_hash":   true,
		"apikey":           true,
		"authorization":    true,
		"credential":       true,
		"credential_hash":  true,
		"credential_value": true,
		"password":         true,
		"password_hash":    true,
		"private_key":      true,
		"refresh_token":    true,
		"secret":           true,
		"secret_hash":      true,
		"secret_value":     true,
		"token":            true,
		"token_hash":       true,
	}
	for _, hint := range hints {
		for _, field := range hint.SensitiveFields {
			normalized := normalizeConnectorOutputField(field)
			if normalized != "" {
				fields[normalized] = true
			}
		}
	}
	return fields
}

func connectorOutputFieldSensitive(key string, sensitiveFields map[string]bool) bool {
	normalized := normalizeConnectorOutputField(key)
	if normalized == "" {
		return false
	}
	if sensitiveFields[normalized] {
		return true
	}
	for field := range sensitiveFields {
		if strings.HasSuffix(normalized, "."+field) || strings.HasSuffix(normalized, "_"+field) {
			return true
		}
	}
	return false
}

func normalizeConnectorOutputField(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	return value
}

func connectorApprovalContext(prepared actions.PreparedRequest, token tokens.Token, permission connectortargets.ActionPermission, capturedAt string) (string, string, error) {
	payloadHashMaterial, err := json.Marshal(map[string]any{
		"input":   prepared.Requested.Input,
		"payload": prepared.Action.Payload,
	})
	if err != nil {
		return "", "", err
	}
	actionDefinition := map[string]any{
		"name":         prepared.ActionDefinition.Name,
		"label":        prepared.ActionDefinition.Label,
		"description":  prepared.ActionDefinition.Description,
		"category":     prepared.ActionDefinition.Category,
		"risk":         prepared.ActionDefinition.Risk,
		"input_schema": prepared.ActionDefinition.InputSchema,
		"output_hint":  prepared.ActionDefinition.OutputHint,
	}
	actionDefinitionHashMaterial, err := json.Marshal(actionDefinition)
	if err != nil {
		return "", "", err
	}
	snapshot := map[string]any{
		"schema_version": approvalContextSchemaVersion,
		"captured_at":    capturedAt,
		"tool": map[string]any{
			"name":           connectorActionToolName,
			"schema_version": approvalContextSchemaVersion,
		},
		"connector": map[string]any{
			"kind":    prepared.Target.ConnectorKind,
			"version": prepared.ConnectorVersion,
		},
		"token": map[string]any{
			"id":         token.ID,
			"expires_at": token.ExpiresAt,
			"revoked_at": token.RevokedAt,
		},
		"permission": map[string]any{
			"rule":       permission.ExecutionRule,
			"expires_at": permission.ExpiresAt,
		},
		"target": map[string]any{
			"id":             prepared.Target.ID,
			"ref":            prepared.Target.Ref,
			"connector_kind": prepared.Target.ConnectorKind,
			"name":           prepared.Target.Name,
			"config":         prepared.Target.Config,
		},
		"profile": map[string]any{
			"id":              prepared.Profile.ID,
			"kind":            prepared.Profile.Kind,
			"label":           prepared.Profile.Label,
			"risk_label":      prepared.Profile.RiskLabel,
			"updated_at":      prepared.Profile.UpdatedAt,
			"secret_revision": prepared.Profile.SecretRevision,
			"public":          prepared.Profile.Public,
		},
		"action": map[string]any{
			"name":            prepared.Action.ActionName,
			"risk":            prepared.Action.Risk,
			"definition":      actionDefinition,
			"definition_hash": sha256Hex(string(actionDefinitionHashMaterial)),
			"payload_hash":    sha256Hex(string(payloadHashMaterial)),
		},
	}
	hash, err := hashGenericApprovalContext(snapshot)
	if err != nil {
		return "", "", err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return "", "", err
	}
	return string(payload), hash, nil
}

func hashGenericApprovalContext(snapshot map[string]any) (string, error) {
	clone := map[string]any{}
	for key, value := range snapshot {
		if key == "captured_at" {
			continue
		}
		clone[key] = value
	}
	payload, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	return sha256Hex(string(payload)), nil
}

func connectorApprovalDriftReason(previousContext string, currentContext string) string {
	var previous map[string]any
	var current map[string]any
	if err := json.Unmarshal([]byte(previousContext), &previous); err != nil {
		return "unknown"
	}
	if err := json.Unmarshal([]byte(currentContext), &current); err != nil {
		return "unknown"
	}
	for _, area := range []string{"connector", "token", "permission", "target", "profile"} {
		if !reflect.DeepEqual(previous[area], current[area]) {
			return area
		}
	}
	if !reflect.DeepEqual(approvalActionValue(previous, "definition_hash"), approvalActionValue(current, "definition_hash")) {
		return "action_definition"
	}
	if !reflect.DeepEqual(approvalActionValue(previous, "payload_hash"), approvalActionValue(current, "payload_hash")) {
		return "payload"
	}
	if !reflect.DeepEqual(previous["action"], current["action"]) {
		return "action"
	}
	return "unknown"
}

func approvalActionValue(snapshot map[string]any, key string) any {
	action, _ := snapshot["action"].(map[string]any)
	if action == nil {
		return nil
	}
	return action[key]
}
