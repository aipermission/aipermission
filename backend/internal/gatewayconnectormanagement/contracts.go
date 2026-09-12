package gatewayconnectormanagement

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type ActionPermissionRule string

const (
	ActionPermissionAlwaysRun        ActionPermissionRule = "always_run"
	ActionPermissionApprovalRequired ActionPermissionRule = "approval_required"
	ActionPermissionBlocked          ActionPermissionRule = "blocked"
)

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

type CredentialProfile struct {
	ID                  int64
	TargetID            int64
	ConnectorKind       string
	Kind                string
	Label               string
	Public              map[string]any
	EncryptedSecretJSON string
	RiskLabel           string
	SecretRevision      int64
	CreatedAt           string
	UpdatedAt           string
}

type RuntimeSurface struct {
	ID             int64
	ConnectorKind  string
	TargetID       int64
	ProfileID      int64
	CapabilityKind string
	Label          string
	Status         string
	CreatedAt      string
	UpdatedAt      string
}

type Target struct {
	ID            int64
	ProjectID     int64
	ProjectName   string
	ProjectSlug   string
	ConnectorKind string
	Name          string
	Config        map[string]any
	Status        string
	CreatedAt     string
	UpdatedAt     string
}

func actionPermissionFromDomain(value connectortargets.ActionPermission) ActionPermission {
	return ActionPermission{
		TokenID: value.TokenID, ProjectID: value.ProjectID, ProjectName: value.ProjectName,
		ProjectSlug: value.ProjectSlug, ProjectEnabled: value.ProjectEnabled,
		TargetID: value.TargetID, TargetName: value.TargetName, ProfileID: value.ProfileID,
		ProfileLabel: value.ProfileLabel, ConnectorKind: value.ConnectorKind,
		ProfileKind: value.ProfileKind, ActionName: value.ActionName,
		ExecutionRule: ActionPermissionRule(value.ExecutionRule), ExpiresAt: value.ExpiresAt,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func AdoptActionPermission(value connectortargets.ActionPermission) ActionPermission {
	return actionPermissionFromDomain(value)
}

func ReleaseActionPermission(value ActionPermission) connectortargets.ActionPermission {
	return connectortargets.ActionPermission{
		TokenID: value.TokenID, ProjectID: value.ProjectID, ProjectName: value.ProjectName,
		ProjectSlug: value.ProjectSlug, ProjectEnabled: value.ProjectEnabled,
		TargetID: value.TargetID, TargetName: value.TargetName, ProfileID: value.ProfileID,
		ProfileLabel: value.ProfileLabel, ConnectorKind: value.ConnectorKind,
		ProfileKind: value.ProfileKind, ActionName: value.ActionName,
		ExecutionRule: connectortargets.ActionPermissionRule(value.ExecutionRule), ExpiresAt: value.ExpiresAt,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func AdoptActionRequest(value connectortargets.ActionRequest) ActionRequest {
	return actionRequestFromDomain(value)
}

func ReleaseActionRequest(value ActionRequest) connectortargets.ActionRequest {
	return value.domain()
}

func AdoptTarget(value connectortargets.Target) Target { return targetFromDomain(value) }

func ReleaseTarget(value Target) connectortargets.Target { return value.domain() }

func AdoptRuntimeSurface(value connectortargets.RuntimeSurface) RuntimeSurface {
	return runtimeSurfaceFromDomain(value)
}

func AdoptCredentialProfile(value connectortargets.CredentialProfile) CredentialProfile {
	return credentialProfileFromDomain(value)
}

func ReleaseCredentialProfile(value CredentialProfile) connectortargets.CredentialProfile {
	return value.domain()
}

func DomainActionFinish(callback func(context.Context, int64, connectors.ResultStatus, any, string, string, ...connectors.OutputHint) (ActionRequest, error)) func(context.Context, int64, connectors.ResultStatus, any, string, string, ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	return func(ctx context.Context, requestID int64, status connectors.ResultStatus, output any, displayText, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
		request, err := callback(ctx, requestID, status, output, displayText, errorText, hints...)
		return ReleaseActionRequest(request), err
	}
}

func DomainActionResponse(callback func(ActionRequest, connectors.ActionResult, bool) any) func(connectortargets.ActionRequest, connectors.ActionResult, bool) any {
	return func(request connectortargets.ActionRequest, result connectors.ActionResult, replayed bool) any {
		return callback(AdoptActionRequest(request), result, replayed)
	}
}

func DomainRunningHint(callback func(ActionRequest) string) func(connectortargets.ActionRequest) string {
	return func(request connectortargets.ActionRequest) string {
		return callback(AdoptActionRequest(request))
	}
}

func DomainTargetDelete(callback func(context.Context, Target, map[string]any) error) func(context.Context, connectortargets.Target, map[string]any) error {
	return func(ctx context.Context, target connectortargets.Target, payload map[string]any) error {
		return callback(ctx, AdoptTarget(target), payload)
	}
}

func DomainTargetFinalize(callback func(context.Context, Target, string, map[string]any) (int64, error)) func(context.Context, connectortargets.Target, string, map[string]any) (int64, error) {
	return func(ctx context.Context, target connectortargets.Target, reason string, payload map[string]any) (int64, error) {
		return callback(ctx, AdoptTarget(target), reason, payload)
	}
}

func actionRequestFromDomain(value connectortargets.ActionRequest) ActionRequest {
	return ActionRequest{
		ID: value.ID, TokenID: cloneInt64(value.TokenID), TokenName: value.TokenName,
		TargetID: value.TargetID, TargetName: value.TargetName, ProfileID: value.ProfileID,
		ProfileLabel: value.ProfileLabel, ConnectorKind: value.ConnectorKind,
		ActionName: value.ActionName, Title: value.Title, Summary: value.Summary,
		Preview: cloneMap(value.Preview), Source: value.Source, Input: cloneMap(value.Input),
		EncryptedPayloadJSON: value.EncryptedPayloadJSON, Reason: value.Reason, Status: value.Status,
		Output: cloneStructured(value.Output), DisplayText: value.DisplayText, Error: value.Error,
		ApprovalContext: value.ApprovalContext, ApprovalContextHash: value.ApprovalContextHash,
		ApprovalContextDrift: value.ApprovalContextDrift, RetryPolicy: cloneRetryPolicy(value.RetryPolicy),
		IdempotencyKey: value.IdempotencyKey, IdempotencyIdentityHash: value.IdempotencyIdentityHash,
		SessionID: cloneInt64(value.SessionID), SessionGeneration: cloneInt64(value.SessionGeneration),
		ExecutionOwner: value.ExecutionOwner, ExecutionLeaseExpiresAt: value.ExecutionLeaseExpiresAt,
		DispatchStartedAt: value.DispatchStartedAt, CreatedAt: value.CreatedAt,
		CompletedAt: cloneString(value.CompletedAt),
	}
}

func (value ActionRequest) domain() connectortargets.ActionRequest {
	return connectortargets.ActionRequest{
		ID: value.ID, TokenID: cloneInt64(value.TokenID), TokenName: value.TokenName,
		TargetID: value.TargetID, TargetName: value.TargetName, ProfileID: value.ProfileID,
		ProfileLabel: value.ProfileLabel, ConnectorKind: value.ConnectorKind,
		ActionName: value.ActionName, Title: value.Title, Summary: value.Summary,
		Preview: cloneMap(value.Preview), Source: value.Source, Input: cloneMap(value.Input),
		EncryptedPayloadJSON: value.EncryptedPayloadJSON, Reason: value.Reason, Status: value.Status,
		Output: cloneStructured(value.Output), DisplayText: value.DisplayText, Error: value.Error,
		ApprovalContext: value.ApprovalContext, ApprovalContextHash: value.ApprovalContextHash,
		ApprovalContextDrift: value.ApprovalContextDrift, RetryPolicy: cloneRetryPolicy(value.RetryPolicy),
		IdempotencyKey: value.IdempotencyKey, IdempotencyIdentityHash: value.IdempotencyIdentityHash,
		SessionID: cloneInt64(value.SessionID), SessionGeneration: cloneInt64(value.SessionGeneration),
		ExecutionOwner: value.ExecutionOwner, ExecutionLeaseExpiresAt: value.ExecutionLeaseExpiresAt,
		DispatchStartedAt: value.DispatchStartedAt, CreatedAt: value.CreatedAt,
		CompletedAt: cloneString(value.CompletedAt),
	}
}

func credentialProfileFromDomain(value connectortargets.CredentialProfile) CredentialProfile {
	return CredentialProfile{
		ID: value.ID, TargetID: value.TargetID, ConnectorKind: value.ConnectorKind,
		Kind: value.Kind, Label: value.Label, Public: cloneMap(value.Public),
		EncryptedSecretJSON: value.EncryptedSecretJSON, RiskLabel: value.RiskLabel,
		SecretRevision: value.SecretRevision, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func (value CredentialProfile) domain() connectortargets.CredentialProfile {
	return connectortargets.CredentialProfile{
		ID: value.ID, TargetID: value.TargetID, ConnectorKind: value.ConnectorKind,
		Kind: value.Kind, Label: value.Label, Public: cloneMap(value.Public),
		EncryptedSecretJSON: value.EncryptedSecretJSON, RiskLabel: value.RiskLabel,
		SecretRevision: value.SecretRevision, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func runtimeSurfaceFromDomain(value connectortargets.RuntimeSurface) RuntimeSurface {
	return RuntimeSurface{
		ID: value.ID, ConnectorKind: value.ConnectorKind, TargetID: value.TargetID,
		ProfileID: value.ProfileID, CapabilityKind: value.CapabilityKind, Label: value.Label,
		Status: string(value.Status), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func targetFromDomain(value connectortargets.Target) Target {
	return Target{
		ID: value.ID, ProjectID: value.ProjectID, ProjectName: value.ProjectName,
		ProjectSlug: value.ProjectSlug, ConnectorKind: value.ConnectorKind, Name: value.Name,
		Config: cloneMap(value.Config), Status: string(value.Status),
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func (value Target) domain() connectortargets.Target {
	return connectortargets.Target{
		ID: value.ID, ProjectID: value.ProjectID, ProjectName: value.ProjectName,
		ProjectSlug: value.ProjectSlug, ConnectorKind: value.ConnectorKind, Name: value.Name,
		Config: cloneMap(value.Config), Status: connectortargets.TargetStatus(value.Status),
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = cloneStructured(item)
	}
	return cloned
}

func cloneStructured(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneStructured(item)
		}
		return cloned
	case []string:
		return append([]string(nil), typed...)
	case []byte:
		return append([]byte(nil), typed...)
	default:
		return value
	}
}

func cloneRetryPolicy(value connectors.RetryPolicy) connectors.RetryPolicy {
	value.PreconditionFields = append([]string(nil), value.PreconditionFields...)
	return value
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
