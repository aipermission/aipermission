package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

const (
	connectorActionToolName          = "connector.call_action"
	connectorActionApprovalHint      = "Wait 3 seconds, then poll this connector action request until it is completed, failed, declined, stale, or blocked."
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
		finished, finishErr := store.FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
			ID:     request.ID,
			Status: connectors.ResultFailed,
			Error:  err.Error(),
		})
		if finishErr != nil {
			return connectorActionCallResult{}, finishErr
		}
		return connectorActionCallResult{Request: finished, Permission: permission, Result: connectors.ActionResult{Status: connectors.ResultFailed, Error: err.Error()}}, nil
	}
	status := result.Status
	if status == "" {
		status = connectors.ResultCompleted
	}
	if status == connectors.ResultRunning || status == connectors.ResultApprovalPending {
		status = connectors.ResultFailed
		result.Error = "connector returned a non-terminal result for synchronous execution"
	}
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
	return connectorActionCallResult{Request: finished, Permission: permission, Result: result}, nil
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
	payload, err := runtime.vault.EncryptJSON(prepared.Action.Payload)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	capturedAt := time.Now().UTC().Format(time.RFC3339)
	approvalContext, approvalHash, err := connectorApprovalContext(prepared, tokenID, permission, capturedAt)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	request, err := connectortargets.NewStore(runtime.database).InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &tokenID,
		TargetID:             prepared.Target.ID,
		ProfileID:            prepared.Profile.ID,
		ConnectorKind:        prepared.Target.ConnectorKind,
		ActionName:           prepared.Action.ActionName,
		Input:                prepared.Requested.Input,
		EncryptedPayloadJSON: payload,
		Reason:               prepared.Requested.Reason,
		Status:               status,
		ApprovalContext:      approvalContext,
		ApprovalContextHash:  approvalHash,
	})
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	if errorText != "" {
		return connectortargets.NewStore(runtime.database).FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
			ID:     request.ID,
			Status: status,
			Error:  errorText,
		})
	}
	return request, nil
}

func (s *Server) executePreparedConnectorAction(ctx context.Context, runtime *databaseRuntime, prepared actions.PreparedRequest) (connectors.ActionResult, error) {
	registry, err := builtin.NewRegistry()
	if err != nil {
		return connectors.ActionResult{}, err
	}
	connector, ok := registry.Get(prepared.Target.ConnectorKind)
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
		Target:  prepared.Target,
		Profile: prepared.Profile,
		Secrets: connectorSecretAccessor{values: secrets},
		Events:  noopConnectorEventSink{},
	}, prepared.Action)
}

func connectorApprovalContext(prepared actions.PreparedRequest, tokenID int64, permission connectortargets.ActionPermission, capturedAt string) (string, string, error) {
	payloadHashMaterial, err := json.Marshal(map[string]any{
		"input":   prepared.Requested.Input,
		"payload": prepared.Action.Payload,
	})
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
		"token": map[string]any{
			"id": tokenID,
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
			"id":     prepared.Profile.ID,
			"kind":   prepared.Profile.Kind,
			"label":  prepared.Profile.Label,
			"public": prepared.Profile.Public,
		},
		"action": map[string]any{
			"name":         prepared.Action.ActionName,
			"risk":         prepared.Action.Risk,
			"payload_hash": sha256Hex(string(payloadHashMaterial)),
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
