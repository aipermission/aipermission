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
func Close(runtime workspaceruntime.Port, resolveActions ActionWorkflowResolver) error {
	if runtime == nil {
		return nil
	}
	if retention := runtime.ObservationPort().RetentionService(); retention != nil {
		retention.Stop()
	}
	stopConnectorActions(runtime, resolveActions)
	if leases := runtime.SecurityPort().VaultLeaseStore(); leases != nil {
		leases.Clear()
	}
	if sessions := runtime.ConnectorPort().ConsoleSessionManager(); sessions != nil {
		sessions.CloseAll()
	}
	operations := runtime.OperationsPort()
	if requests := operations.CommandRequestRuntime(); requests != nil {
		if err := requests.CancelRunning(context.Background(), runtimeoutcome.CommandCanceled); err != nil {
			log.Printf("mark running command requests failed workspace=%s error=%v", runtime.DatabaseIdentifier(), err)
		}
	}
	if transfers := operations.FileTransferRuntime(); transfers == nil {
		operations.FileTransferLifecycle().Stop()
		log.Printf("file transfer shutdown runtime unavailable workspace=%s", runtime.DatabaseIdentifier())
	} else {
		drained, err := transfers.Shutdown(
			transferWait, runtimeoutcome.TransferInterrupted, runtimeoutcome.TransferQueueStopped,
		)
		if err != nil {
			log.Printf("mark running file transfers failed workspace=%s error=%v", runtime.DatabaseIdentifier(), err)
		}
		if !drained {
			go func() {
				operations.FileTransferLifecycle().Wait(context.Background())
				if err := closeStorage(runtime); err != nil {
					log.Printf("deferred runtime storage close failed workspace=%s error=%v", runtime.DatabaseIdentifier(), err)
				}
			}()
			return fmt.Errorf("file transfer shutdown exceeded %s; runtime storage close deferred until workers exit", transferWait)
		}
	}
	return closeStorage(runtime)
}

// Discard releases a partially opened runtime without running normal shutdown
// recovery against state that was never published.
func Discard(runtime workspaceruntime.Port) error {
	if runtime == nil {
		return nil
	}
	runtime.OperationsPort().FileTransferLifecycle().Stop()
	return closeStorage(runtime)
}

func stopConnectorActions(runtime workspaceruntime.Port, resolve ActionWorkflowResolver) {
	var workflow ActionWorkflow
	if existing := runtime.OperationsPort().ActionWorkflow(); existing != nil {
		workflow = existing
	}
	if workflow == nil {
		if resolve == nil {
			return
		}
		var err error
		workflow, err = resolve()
		if err != nil {
			log.Printf("initialize connector action shutdown workspace=%s error=%v", runtime.DatabaseIdentifier(), err)
			return
		}
	}
	workflow.StopRecovery()
	if err := workflow.MarkRunningOutcomeUnknown(context.Background(), runtimeoutcome.ConnectorActionUnknown); err != nil {
		log.Printf("mark running connector actions outcome unknown failed workspace=%s error=%v", runtime.DatabaseIdentifier(), err)
	}
}

func closeStorage(runtime workspaceruntime.Port) error {
	if dispatcher := runtime.ObservationPort().AuditDispatcherService(); dispatcher != nil {
		dispatcher.Stop()
	}
	actions.ClearIdentityKey(runtime.ActionIdentity())
	runtime.ClearActionIdentity()
	storage := runtime.StoragePort()
	var closeErrors []error
	if database := storage.DatabaseHandle(); database != nil {
		if err := database.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close encrypted database runtime %q: %w", runtime.DatabaseIdentifier(), err))
		}
	}
	if ownership := storage.DatabaseOwnership(); ownership != nil {
		if err := ownership.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("release encrypted database runtime %q ownership: %w", runtime.DatabaseIdentifier(), err))
		}
		storage.ClearDatabaseOwnership()
	}
	return errors.Join(closeErrors...)
}
