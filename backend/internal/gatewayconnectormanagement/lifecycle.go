package gatewayconnectormanagement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

const deletedTargetFinalizationTimeout = 10 * time.Second

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
	Finalizations   LifecycleFinalizationStore
}

type LifecycleFinalizationStore interface {
	Pending(context.Context) ([]connectortargets.PendingLifecycleFinalization, error)
	MarkVaultComplete(context.Context, int64) error
	MarkRequestsComplete(context.Context, int64) error
	RecordAttempt(context.Context, int64) error
}

type LifecycleService struct {
	dependencies LifecycleServiceDependencies
}

func NewLifecycleService(dependencies LifecycleServiceDependencies) *LifecycleService {
	return &LifecycleService{dependencies: dependencies}
}

func NewLifecycleFinalizationStore(database *sql.DB) LifecycleFinalizationStore {
	return connectortargets.NewLifecycleFinalizationStore(database)
}

func (service *LifecycleService) DeleteTarget(
	ctx context.Context,
	target Target,
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
		func(tx *sql.Tx) error {
			if err := connectortargets.NewTxStore(tx).DeleteTarget(ctx, target.ID); err != nil {
				return err
			}
			return connectortargets.NewLifecycleFinalizationStore(tx).Queue(ctx, connectortargets.LifecycleFinalizationChange{
				TargetID:       target.ID,
				StaleReason:    "connector target was deleted; send a fresh Vault request",
				UserMessage:    "connector target was deleted; ask the AI to send a fresh request",
				IncludeRunning: true,
			})
		},
	)
}

func (service *LifecycleService) FinalizeDeletedTarget(
	ctx context.Context,
	target Target,
	staleReason string,
) (int64, error) {
	if staleReason == "" {
		staleReason = "connector target was deleted; ask the AI to send a fresh request"
	}
	return service.finalizeChange(ctx, TargetLifecycleChange{
		TargetID:       target.ID,
		StaleReason:    "connector target was deleted; send a fresh Vault request",
		UserMessage:    staleReason,
		IncludeRunning: true,
	})
}

func (service *LifecycleService) AfterCredentialChange(
	ctx context.Context,
	change TargetLifecycleChange,
) error {
	_, err := service.finalizeChange(ctx, change)
	return err
}

func (service *LifecycleService) RecoverPending(ctx context.Context) error {
	if err := service.validate(); err != nil {
		return err
	}
	finalizationCtx, cancel := lifecycleFinalizationContext(ctx)
	defer cancel()
	items, err := service.dependencies.Finalizations.Pending(finalizationCtx)
	if err != nil {
		return err
	}
	var finalizationErrors []error
	for _, item := range items {
		if _, err := service.finalizeItem(finalizationCtx, item); err != nil {
			finalizationErrors = append(finalizationErrors, err)
		}
	}
	return errors.Join(finalizationErrors...)
}

func (service *LifecycleService) finalizeChange(ctx context.Context, change TargetLifecycleChange) (int64, error) {
	if err := service.validate(); err != nil {
		return 0, err
	}
	finalizationCtx, cancel := lifecycleFinalizationContext(ctx)
	defer cancel()
	items, err := service.dependencies.Finalizations.Pending(finalizationCtx)
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		if item.Change.TargetID == change.TargetID && item.Change.ProfileID == change.ProfileID {
			return service.finalizeItem(finalizationCtx, item)
		}
	}
	return 0, fmt.Errorf("%w: durable connector lifecycle intent is missing", connectortargets.ErrLifecycleFinalizationPending)
}

func (service *LifecycleService) finalizeItem(ctx context.Context, item connectortargets.PendingLifecycleFinalization) (int64, error) {
	var finalizationErrors []error
	affected := int64(0)
	if item.VaultPending {
		vaultErr := service.invalidateVault(
			ctx, item.Change.TargetID, item.Change.ProfileID, item.Change.StaleReason,
		)
		if vaultErr == nil {
			vaultErr = service.dependencies.Finalizations.MarkVaultComplete(ctx, item.ID)
		}
		if vaultErr != nil {
			finalizationErrors = append(finalizationErrors, fmt.Errorf("finalize Vault sessions: %w", vaultErr))
		}
	}
	if item.RequestsPending {
		var requestErr error
		affected, requestErr = service.InvalidateActionRequests(
			ctx, item.Change.TargetID, item.Change.ProfileID,
			item.Change.UserMessage, item.Change.IncludeRunning,
		)
		if requestErr == nil {
			requestErr = service.dependencies.Finalizations.MarkRequestsComplete(ctx, item.ID)
		}
		if requestErr != nil {
			finalizationErrors = append(finalizationErrors, fmt.Errorf("finalize action requests: %w", requestErr))
		}
	}
	if len(finalizationErrors) > 0 {
		attemptErr := service.dependencies.Finalizations.RecordAttempt(ctx, item.ID)
		return affected, errors.Join(
			connectortargets.ErrLifecycleFinalizationPending,
			errors.Join(finalizationErrors...),
			attemptErr,
		)
	}
	return affected, nil
}

func lifecycleFinalizationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	detached := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		return context.WithDeadline(detached, deadline)
	}
	return context.WithTimeout(detached, deletedTargetFinalizationTimeout)
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
		service.dependencies.InvalidateVault == nil || service.dependencies.Finalizations == nil {
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
