// Package shutdown owns the ordered teardown of one unlocked workspace runtime.
package shutdown

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/runtimeoutcome"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

const transferWait = 10 * time.Second
const shutdownWait = 15 * time.Second
const deferredShutdownWait = 2 * time.Minute

// ActionWorkflow is the action lifecycle needed during workspace teardown.
type ActionWorkflow interface {
	Shutdown(context.Context) error
	MarkRunningOutcomeUnknown(context.Context, string) error
}

type ActionWorkflowResolver func() (ActionWorkflow, error)

type CommandWorkflow interface {
	StopWorkers(context.Context) error
	CancelRunning(context.Context, string) error
}

type CommandWorkflowResolver func() (CommandWorkflow, error)

type TransferWorkflow interface {
	Shutdown(time.Duration, string, string) (bool, bool, error)
	Wait(context.Context) bool
	Abort()
}

type TransferWorkflowResolver func() TransferWorkflow

// Close stops runtime workers and sessions before releasing encrypted storage.
// If transfer workers outlive the bounded wait, storage closes asynchronously
// after they exit so no worker can touch a closed database.
func Close(runtime *workspaceruntime.Runtime, resolveActions ActionWorkflowResolver, resolveCommands CommandWorkflowResolver, resolveTransfers TransferWorkflowResolver) error {
	return closeWithTimeout(runtime, resolveActions, resolveCommands, resolveTransfers, shutdownWait)
}

func closeWithTimeout(runtime *workspaceruntime.Runtime, resolveActions ActionWorkflowResolver, resolveCommands CommandWorkflowResolver, resolveTransfers TransferWorkflowResolver, wait time.Duration) error {
	if runtime == nil {
		return nil
	}
	if retention := runtime.Observation.RetentionService(); retention != nil {
		retention.Stop()
	}
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), wait)
	defer shutdownCancel()
	actionWorkflow, actionsDrained, actionErr := stopConnectorActions(shutdownContext, runtime, resolveActions)
	if actionsDrained {
		clearActionIdentity(runtime)
	}
	commandWorkflow, commandsDrained, commandErr := stopCommandRequests(shutdownContext, runtime.ID, resolveCommands)
	if leases := runtime.Security.VaultLeaseStore(); leases != nil {
		leases.Clear()
	}
	if sessions := runtime.Connectors.ConsoleSessionManager(); sessions != nil {
		sessions.CloseAll()
	}
	transfer, initialized, drained, transferErr := shutdownTransfers(resolveTransfers)
	if transferErr != nil {
		log.Printf("workspace transfer shutdown failed workspace=%s error=%v", runtime.ID, transferErr)
	}
	if !actionsDrained || !commandsDrained || initialized && !drained {
		go func() {
			deferredContext, cancel := context.WithTimeout(context.Background(), deferredShutdownWait)
			defer cancel()
			if !actionsDrained && actionWorkflow != nil {
				if err := actionWorkflow.Shutdown(deferredContext); err != nil {
					log.Printf("deferred connector action drain failed workspace=%s error=%v", runtime.ID, err)
					return
				}
				if err := actionWorkflow.MarkRunningOutcomeUnknown(deferredContext, runtimeoutcome.ConnectorActionUnknown); err != nil {
					log.Printf("deferred connector action persistence failed workspace=%s error=%v", runtime.ID, err)
					return
				}
				clearActionIdentity(runtime)
			}
			if !commandsDrained && commandWorkflow != nil {
				if err := commandWorkflow.StopWorkers(deferredContext); err != nil {
					log.Printf("deferred command worker drain failed workspace=%s error=%v", runtime.ID, err)
					return
				}
				if err := commandWorkflow.CancelRunning(deferredContext, runtimeoutcome.CommandCanceled); err != nil {
					log.Printf("deferred command persistence failed workspace=%s error=%v", runtime.ID, err)
					return
				}
			}
			if initialized && !drained {
				if !transfer.Wait(deferredContext) {
					log.Printf("deferred transfer drain timed out workspace=%s", runtime.ID)
					return
				}
			}
			if err := closeStorage(runtime); err != nil {
				log.Printf("deferred runtime storage close failed workspace=%s error=%v", runtime.ID, err)
			}
		}()
		return errors.Join(actionErr, commandErr, transferErr, fmt.Errorf("workspace shutdown exceeded its bounded wait; runtime storage close deferred until workers exit"))
	}
	return errors.Join(actionErr, commandErr, transferErr, closeStorage(runtime))
}

func shutdownTransfers(resolve TransferWorkflowResolver) (TransferWorkflow, bool, bool, error) {
	if resolve == nil {
		return nil, false, true, nil
	}
	workflow := resolve()
	if workflow == nil {
		return nil, false, true, nil
	}
	initialized, drained, err := workflow.Shutdown(transferWait, "interrupted by workspace shutdown", "queue stopped by workspace shutdown")
	return workflow, initialized, drained, err
}

func stopCommandRequests(ctx context.Context, workspaceID string, resolve CommandWorkflowResolver) (CommandWorkflow, bool, error) {
	if resolve == nil {
		return nil, true, nil
	}
	requests, err := resolve()
	if err != nil {
		log.Printf("initialize command request shutdown workspace=%s error=%v", workspaceID, err)
		return nil, true, err
	}
	if requests == nil {
		return nil, true, nil
	}
	if err := requests.StopWorkers(ctx); err != nil {
		log.Printf("stop command workers failed workspace=%s error=%v", workspaceID, err)
		return requests, false, err
	}
	if err := requests.CancelRunning(ctx, runtimeoutcome.CommandCanceled); err != nil {
		log.Printf("mark running command requests failed workspace=%s error=%v", workspaceID, err)
		return requests, true, err
	}
	return requests, true, nil
}

// Discard releases a partially opened runtime without running normal shutdown
// recovery against state that was never published.
func Discard(runtime *workspaceruntime.Runtime, resolveTransfers TransferWorkflowResolver) error {
	if runtime == nil {
		return nil
	}
	if resolveTransfers != nil {
		if transfer := resolveTransfers(); transfer != nil {
			transfer.Abort()
		}
	}
	return closeStorage(runtime)
}

func stopConnectorActions(ctx context.Context, runtime *workspaceruntime.Runtime, resolve ActionWorkflowResolver) (ActionWorkflow, bool, error) {
	if resolve == nil {
		return nil, true, nil
	}
	workflow, err := resolve()
	if err != nil {
		log.Printf("initialize connector action shutdown workspace=%s error=%v", runtime.ID, err)
		return nil, true, err
	}
	if workflow == nil {
		return nil, true, nil
	}
	if err := workflow.Shutdown(ctx); err != nil {
		log.Printf("stop connector action workers workspace=%s error=%v", runtime.ID, err)
		return workflow, false, err
	}
	if err := workflow.MarkRunningOutcomeUnknown(ctx, runtimeoutcome.ConnectorActionUnknown); err != nil {
		log.Printf("mark running connector actions outcome unknown failed workspace=%s error=%v", runtime.ID, err)
		return workflow, true, err
	}
	return workflow, true, nil
}

func clearActionIdentity(runtime *workspaceruntime.Runtime) {
	if runtime == nil {
		return
	}
	actions.ClearIdentityKey(runtime.ActionIdentityKey)
	runtime.ActionIdentityKey = nil
}

func closeStorage(runtime *workspaceruntime.Runtime) error {
	if dispatcher := runtime.Observation.AuditDispatcherService(); dispatcher != nil {
		dispatcher.Stop()
	}
	clearActionIdentity(runtime)
	storage := &runtime.Storage
	var closeErrors []error
	if database := storage.DatabaseHandle(); database != nil {
		if err := database.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close encrypted database runtime %q: %w", runtime.ID, err))
		}
	}
	if ownership := storage.DatabaseOwnership(); ownership != nil {
		if err := ownership.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("release encrypted database runtime %q ownership: %w", runtime.ID, err))
		}
		storage.ClearDatabaseOwnership()
	}
	return errors.Join(closeErrors...)
}
