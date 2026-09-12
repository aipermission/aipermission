package vaultrequests

import (
	"context"
	"fmt"
	"time"
)

// WorkflowPorts are the gateway-owned effects needed by the Vault request
// lifecycle. The workflow owns sequencing; adapters retain database, audit,
// encryption, and connector runtime details.
type WorkflowPorts struct {
	Claim               func(context.Context, int64) (Request, error)
	Execute             func(context.Context, Request) (any, error)
	ExecuteAtomic       func(context.Context, Request) (WorkflowResult, bool, error)
	Complete            func(context.Context, int64, string, any, string) (Request, error)
	Get                 func(context.Context, int64) (Request, error)
	Repair              func(context.Context, int64) error
	Compensate          func(context.Context, Request, any) error
	RedactError         func(context.Context, error) string
	IsStale             func(error) bool
	FinalizationTimeout time.Duration
}

// WorkflowResult returns the persisted request and any connector-side
// execution failure separately. A persisted failed/stale result is therefore
// not confused with a workflow infrastructure failure.
type WorkflowResult struct {
	Request        Request
	ExecutionError error
}

// RunWorkflow claims, executes, finalizes, repairs, and when necessary
// compensates one Vault action request.
func RunWorkflow(ctx context.Context, requestID int64, ports WorkflowPorts) (WorkflowResult, error) {
	if err := validateWorkflowPorts(ports, true); err != nil {
		return WorkflowResult{}, err
	}
	item, err := ports.Claim(ctx, requestID)
	if err != nil {
		return WorkflowResult{}, err
	}
	return RunClaimedWorkflow(ctx, item, ports)
}

// RunClaimedWorkflow executes an action request created directly in the
// running state by an Always permission. It shares the exact finalization and
// compensation behavior used by approved requests.
func RunClaimedWorkflow(ctx context.Context, item Request, ports WorkflowPorts) (WorkflowResult, error) {
	if err := validateWorkflowPorts(ports, false); err != nil {
		return WorkflowResult{}, err
	}
	if ports.ExecuteAtomic != nil {
		result, handled, err := ports.ExecuteAtomic(ctx, item)
		if handled || err != nil {
			return result, err
		}
	}
	output, executeErr := ports.Execute(ctx, item)
	finalizeCtx := context.WithoutCancel(ctx)
	if ports.FinalizationTimeout > 0 {
		var cancel context.CancelFunc
		finalizeCtx, cancel = context.WithTimeout(finalizeCtx, ports.FinalizationTimeout)
		defer cancel()
	}
	status := StatusCompleted
	errorText := ""
	if executeErr != nil {
		status = StatusFailed
		errorText = ports.RedactError(finalizeCtx, executeErr)
		if ports.IsStale(executeErr) {
			status = StatusStale
		}
	}
	completed, err := ports.Complete(finalizeCtx, item.ID, status, output, errorText)
	if err == nil {
		return WorkflowResult{Request: completed, ExecutionError: executeErr}, nil
	}
	current, getErr := ports.Get(finalizeCtx, item.ID)
	if getErr == nil && current.Status == status {
		if repairErr := ports.Repair(finalizeCtx, item.ID); repairErr != nil {
			return WorkflowResult{}, fmt.Errorf("repair finalized Vault request projection: %w", repairErr)
		}
		return WorkflowResult{Request: current, ExecutionError: executeErr}, nil
	}
	if compensateErr := ports.Compensate(finalizeCtx, item, output); compensateErr != nil {
		return WorkflowResult{}, fmt.Errorf("finalize Vault action: %w; compensate effect: %v", err, compensateErr)
	}
	failure := "Vault action effect was rolled back because request finalization failed"
	failed, failErr := ports.Complete(finalizeCtx, item.ID, StatusFailed, nil, failure)
	if failErr != nil {
		return WorkflowResult{}, fmt.Errorf("finalize Vault action: %w; record compensation: %v", err, failErr)
	}
	return WorkflowResult{Request: failed, ExecutionError: fmt.Errorf("%s", failure)}, nil
}

func validateWorkflowPorts(ports WorkflowPorts, requireClaim bool) error {
	if (requireClaim && ports.Claim == nil) || ports.Execute == nil || ports.Complete == nil || ports.Get == nil ||
		ports.Repair == nil || ports.Compensate == nil || ports.RedactError == nil || ports.IsStale == nil {
		return fmt.Errorf("Vault request workflow is not configured")
	}
	return nil
}
