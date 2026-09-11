package gatewayconnectormanagement

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

var (
	ErrLifecycleUnavailable       = errors.New("connector lifecycle service is unavailable")
	errLifecycleMutationUnchanged = errors.New("connector lifecycle mutation unchanged")
)

type AuditedMutation func(
	context.Context,
	string,
	string,
	func() any,
	func(*sql.Tx) error,
) error

type LifecycleServiceDependencies struct {
	Mutate          AuditedMutation
	Redact          func(context.Context, string) string
	InvalidateVault func(context.Context, int64, int64, string) error
}

type LifecycleService struct {
	dependencies LifecycleServiceDependencies
}

func NewLifecycleService(dependencies LifecycleServiceDependencies) *LifecycleService {
	return &LifecycleService{dependencies: dependencies}
}

func (service *LifecycleService) DeleteTarget(
	ctx context.Context,
	target connectortargets.Target,
	payload map[string]any,
) error {
	if err := service.validate(); err != nil {
		return err
	}
	payload = cloneLifecyclePayload(payload)
	payload["target_id"] = target.ID
	payload["connector_kind"] = target.ConnectorKind
	payload["name"] = target.Name
	return service.dependencies.Mutate(
		ctx,
		"user",
		"connector.target.deleted",
		func() any { return payload },
		func(tx *sql.Tx) error { return connectortargets.NewTxStore(tx).DeleteTarget(ctx, target.ID) },
	)
}

func (service *LifecycleService) FinalizeDeletedTarget(
	ctx context.Context,
	target connectortargets.Target,
	staleReason string,
) (int64, error) {
	if staleReason == "" {
		staleReason = "connector target was deleted; ask the AI to send a fresh request"
	}
	if err := service.invalidateVault(
		ctx, target.ID, 0, "connector target was deleted; send a fresh Vault request",
	); err != nil {
		return 0, err
	}
	return service.InvalidateActionRequests(ctx, target.ID, 0, staleReason, true)
}

func (service *LifecycleService) AfterCredentialChange(
	ctx context.Context,
	change connectormanagement.TargetLifecycleChange,
) error {
	if err := service.invalidateVault(ctx, change.TargetID, change.ProfileID, change.StaleReason); err != nil {
		return err
	}
	_, err := service.InvalidateActionRequests(
		ctx, change.TargetID, change.ProfileID, change.UserMessage, change.IncludeRunning,
	)
	return err
}

func (service *LifecycleService) InvalidateActionRequests(
	ctx context.Context,
	targetID int64,
	profileID int64,
	reason string,
	includeRunning bool,
) (int64, error) {
	if err := service.validate(); err != nil || targetID < 1 {
		return 0, err
	}
	input := connectortargets.InvalidateActionRequestsForTargetInput{
		TargetID:  targetID,
		ProfileID: profileID,
		Error:     service.dependencies.Redact(ctx, reason),
		RunningError: service.dependencies.Redact(
			ctx,
			"connector configuration changed after dispatch; the external outcome is unknown and must be inspected before retrying",
		),
		ApprovalDrift:  lifecycleApprovalDrift(profileID),
		IncludeRunning: includeRunning,
	}
	var result connectortargets.InvalidateActionRequestsForTargetResult
	err := service.dependencies.Mutate(
		ctx,
		"gateway",
		"connector_action.requests.invalidated",
		func() any {
			return map[string]any{
				"target_id":                   targetID,
				"profile_id":                  profileID,
				"request_ids":                 result.IDs,
				"stale_request_ids":           result.StaleIDs,
				"outcome_unknown_request_ids": result.OutcomeUnknownIDs,
				"affected":                    result.Affected,
			}
		},
		func(tx *sql.Tx) error {
			var err error
			result, err = connectortargets.NewTxStore(tx).InvalidateActionRequestsForTarget(ctx, input)
			if err == nil && result.Affected == 0 {
				return errLifecycleMutationUnchanged
			}
			return err
		},
	)
	if errors.Is(err, errLifecycleMutationUnchanged) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return result.Affected, nil
}

func (service *LifecycleService) invalidateVault(
	ctx context.Context,
	targetID int64,
	profileID int64,
	reason string,
) error {
	if err := service.validate(); err != nil {
		return err
	}
	return service.dependencies.InvalidateVault(ctx, targetID, profileID, reason)
}

func (service *LifecycleService) validate() error {
	if service == nil || service.dependencies.Mutate == nil || service.dependencies.Redact == nil ||
		service.dependencies.InvalidateVault == nil {
		return ErrLifecycleUnavailable
	}
	return nil
}

func lifecycleApprovalDrift(profileID int64) string {
	if profileID > 0 {
		return "profile"
	}
	return "target"
}

func cloneLifecyclePayload(payload map[string]any) map[string]any {
	cloned := make(map[string]any, len(payload)+3)
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}
