// Package shutdown owns the ordered teardown of one unlocked workspace runtime.
package shutdown

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/componentstate"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtimecontract"
	"github.com/aipermission/aipermission/backend/internal/runtimeoutcome"
)

const transferWait = 10 * time.Second

// ActionWorkflow is the action lifecycle needed during workspace teardown.
type ActionWorkflow interface {
	StopRecovery()
	MarkRunningOutcomeUnknown(context.Context, string) error
}

type ActionWorkflowResolver func() (ActionWorkflow, error)

type CommandWorkflow interface {
	CancelRunning(context.Context, string) error
}

type CommandWorkflowResolver func() (CommandWorkflow, error)

// Close stops runtime workers and sessions before releasing encrypted storage.
// If transfer workers outlive the bounded wait, storage closes asynchronously
// after they exit so no worker can touch a closed database.
func Close(runtime runtimecontract.Runtime, resolveActions ActionWorkflowResolver, resolveCommands CommandWorkflowResolver) error {
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
	stopCommandRequests(runtime.DatabaseIdentifier(), resolveCommands)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), transferWait)
	report := componentstate.CloseComponents(shutdownCtx, runtime.ComponentStatePort())
	cancel()
	if report.Err != nil {
		log.Printf("workspace component shutdown failed workspace=%s error=%v", runtime.DatabaseIdentifier(), report.Err)
	}
	if !report.Drained {
		go func() {
			componentstate.WaitComponents(context.Background(), runtime.ComponentStatePort())
			if err := closeStorage(runtime); err != nil {
				log.Printf("deferred runtime storage close failed workspace=%s error=%v", runtime.DatabaseIdentifier(), err)
			}
		}()
		return fmt.Errorf("file transfer shutdown exceeded %s; runtime storage close deferred until workers exit", transferWait)
	}
	return closeStorage(runtime)
}

func stopCommandRequests(workspaceID string, resolve CommandWorkflowResolver) {
	if resolve == nil {
		return
	}
	requests, err := resolve()
	if err != nil {
		log.Printf("initialize command request shutdown workspace=%s error=%v", workspaceID, err)
		return
	}
	if requests == nil {
		return
	}
	if err := requests.CancelRunning(context.Background(), runtimeoutcome.CommandCanceled); err != nil {
		log.Printf("mark running command requests failed workspace=%s error=%v", workspaceID, err)
	}
}

// Discard releases a partially opened runtime without running normal shutdown
// recovery against state that was never published.
func Discard(runtime runtimecontract.Runtime) error {
	if runtime == nil {
		return nil
	}
	_ = componentstate.AbortComponents(runtime.ComponentStatePort())
	return closeStorage(runtime)
}

func stopConnectorActions(runtime runtimecontract.Runtime, resolve ActionWorkflowResolver) {
	if resolve == nil {
		return
	}
	workflow, err := resolve()
	if err != nil {
		log.Printf("initialize connector action shutdown workspace=%s error=%v", runtime.DatabaseIdentifier(), err)
		return
	}
	if workflow == nil {
		return
	}
	workflow.StopRecovery()
	if err := workflow.MarkRunningOutcomeUnknown(context.Background(), runtimeoutcome.ConnectorActionUnknown); err != nil {
		log.Printf("mark running connector actions outcome unknown failed workspace=%s error=%v", runtime.DatabaseIdentifier(), err)
	}
}

func closeStorage(runtime runtimecontract.Runtime) error {
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
