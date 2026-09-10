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

// ActionWorkflow is the action lifecycle needed during workspace teardown.
type ActionWorkflow interface {
	StopRecovery()
	MarkRunningOutcomeUnknown(context.Context, string) error
}

type ActionWorkflowResolver func() (ActionWorkflow, error)

// Close stops runtime workers and sessions before releasing encrypted storage.
// If transfer workers outlive the bounded wait, storage closes asynchronously
// after they exit so no worker can touch a closed database.
func Close(runtime *workspaceruntime.Runtime, resolveActions ActionWorkflowResolver) error {
	if runtime == nil {
		return nil
	}
	if runtime.Observation.Retention != nil {
		runtime.Observation.Retention.Stop()
	}
	stopConnectorActions(runtime, resolveActions)
	if runtime.Security.VaultLeases != nil {
		runtime.Security.VaultLeases.Clear()
	}
	if runtime.Connectors.ConsoleSessions != nil {
		runtime.Connectors.ConsoleSessions.CloseAll()
	}
	if runtime.Operations.CommandRequests != nil {
		if err := runtime.Operations.CommandRequests.CancelRunning(context.Background(), runtimeoutcome.CommandCanceled); err != nil {
			log.Printf("mark running command requests failed workspace=%s error=%v", runtime.ID, err)
		}
	}
	if runtime.Operations.FileTransfers == nil {
		runtime.Operations.TransferLifecycle.Stop()
		log.Printf("file transfer shutdown runtime unavailable workspace=%s", runtime.ID)
	} else {
		drained, err := runtime.Operations.FileTransfers.Shutdown(
			transferWait, runtimeoutcome.TransferInterrupted, runtimeoutcome.TransferQueueStopped,
		)
		if err != nil {
			log.Printf("mark running file transfers failed workspace=%s error=%v", runtime.ID, err)
		}
		if !drained {
			go func() {
				runtime.Operations.TransferLifecycle.Wait(context.Background())
				if err := closeStorage(runtime); err != nil {
					log.Printf("deferred runtime storage close failed workspace=%s error=%v", runtime.ID, err)
				}
			}()
			return fmt.Errorf("file transfer shutdown exceeded %s; runtime storage close deferred until workers exit", transferWait)
		}
	}
	return closeStorage(runtime)
}

// Discard releases a partially opened runtime without running normal shutdown
// recovery against state that was never published.
func Discard(runtime *workspaceruntime.Runtime) error {
	if runtime == nil {
		return nil
	}
	runtime.Operations.TransferLifecycle.Stop()
	return closeStorage(runtime)
}

func stopConnectorActions(runtime *workspaceruntime.Runtime, resolve ActionWorkflowResolver) {
	var workflow ActionWorkflow
	if existing := runtime.Operations.ActionWorkflow(); existing != nil {
		workflow = existing
	}
	if workflow == nil {
		if resolve == nil {
			return
		}
		var err error
		workflow, err = resolve()
		if err != nil {
			log.Printf("initialize connector action shutdown workspace=%s error=%v", runtime.ID, err)
			return
		}
	}
	workflow.StopRecovery()
	if err := workflow.MarkRunningOutcomeUnknown(context.Background(), runtimeoutcome.ConnectorActionUnknown); err != nil {
		log.Printf("mark running connector actions outcome unknown failed workspace=%s error=%v", runtime.ID, err)
	}
}

func closeStorage(runtime *workspaceruntime.Runtime) error {
	if runtime.Observation.AuditDispatcher != nil {
		runtime.Observation.AuditDispatcher.Stop()
	}
	actions.ClearIdentityKey(runtime.ActionIdentityKey)
	runtime.ActionIdentityKey = nil
	var closeErrors []error
	if runtime.Storage.Database != nil {
		if err := runtime.Storage.Database.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close encrypted database runtime %q: %w", runtime.ID, err))
		}
	}
	if runtime.Storage.Ownership != nil {
		if err := runtime.Storage.Ownership.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("release encrypted database runtime %q ownership: %w", runtime.ID, err))
		}
		runtime.Storage.Ownership = nil
	}
	return errors.Join(closeErrors...)
}
