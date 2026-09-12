// Package shutdown owns the ordered teardown of one unlocked workspace runtime.
package shutdown

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aipermission/aipermission/backend/internal/runtimeoutcome"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

const shutdownWait = 15 * time.Second
const deferredShutdownWait = 2 * time.Minute
const deferredRetryWait = 250 * time.Millisecond
const deferredRetryMax = 30 * time.Second

type deferredShutdownError struct{}

func (deferredShutdownError) Error() string {
	return "workspace shutdown is continuing in the background"
}
func (deferredShutdownError) WorkspaceCloseDeferred() {}

var ErrShutdownDeferred error = deferredShutdownError{}

// ActionWorkflow is the action lifecycle needed during workspace teardown.
type ActionWorkflow interface {
	BeginShutdown()
	WaitShutdown(context.Context) error
	MarkRunningOutcomeUnknown(context.Context, string) error
}

type ActionWorkflowResolver func() (ActionWorkflow, error)

type CommandWorkflow interface {
	BeginWorkerShutdown()
	WaitWorkers(context.Context) error
	CancelRunning(context.Context, string) error
}

type CommandWorkflowResolver func() (CommandWorkflow, error)

type TransferWorkflow interface {
	BeginShutdown() (bool, error)
	Wait(context.Context) bool
	Recover(context.Context, string, string) error
	Abort()
}

type TransferWorkflowResolver func() TransferWorkflow

// Close stops runtime workers and sessions before releasing encrypted storage.
// If transfer workers outlive the bounded wait, storage closes asynchronously
// after they exit so no worker can touch a closed database.
func Close(runtime *workspaceruntime.Runtime, resolveActions ActionWorkflowResolver, resolveCommands CommandWorkflowResolver, resolveTransfers TransferWorkflowResolver, onComplete func()) error {
	return closeWithTimeoutAndComplete(runtime, resolveActions, resolveCommands, resolveTransfers, shutdownWait, onComplete)
}

func closeWithTimeout(runtime *workspaceruntime.Runtime, resolveActions ActionWorkflowResolver, resolveCommands CommandWorkflowResolver, resolveTransfers TransferWorkflowResolver, wait time.Duration) error {
	return closeWithTimeoutAndComplete(runtime, resolveActions, resolveCommands, resolveTransfers, wait, nil)
}

func closeWithTimeoutAndComplete(runtime *workspaceruntime.Runtime, resolveActions ActionWorkflowResolver, resolveCommands CommandWorkflowResolver, resolveTransfers TransferWorkflowResolver, wait time.Duration, onComplete func()) error {
	if runtime == nil {
		if onComplete != nil {
			onComplete()
		}
		return nil
	}
	if retention := runtime.Observation.RetentionService(); retention != nil {
		retention.Stop()
	}
	coordinator := &teardownCoordinator{
		runtime: runtime, resolveActions: resolveActions,
		resolveCommands: resolveCommands, resolveTransfers: resolveTransfers,
	}
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), wait)
	defer shutdownCancel()
	ready, drainErr := coordinator.drain(shutdownContext)
	if ready {
		if closed, closeErr := closeStorage(shutdownContext, runtime); closed {
			drainErr = errors.Join(drainErr, closeErr)
			if onComplete != nil {
				onComplete()
			}
			return drainErr
		} else {
			drainErr = errors.Join(drainErr, closeErr)
		}
	}
	runtime.StartTeardown(func() {
		coordinator.run()
		if onComplete != nil {
			onComplete()
		}
	})
	return errors.Join(drainErr, ErrShutdownDeferred)
}

type teardownCoordinator struct {
	runtime             *workspaceruntime.Runtime
	resolveActions      ActionWorkflowResolver
	resolveCommands     CommandWorkflowResolver
	resolveTransfers    TransferWorkflowResolver
	actions             ActionWorkflow
	commands            CommandWorkflow
	transfer            TransferWorkflow
	actionsResolved     bool
	actionsBegun        bool
	commandsResolved    bool
	commandsBegun       bool
	transferResolved    bool
	transferBegun       bool
	actionsDrained      bool
	actionsRecovered    bool
	commandsDrained     bool
	commandsRecovered   bool
	transferInitialized bool
	transferDrained     bool
	transferRecovered   bool
	sessionsDrained     bool
	actionWait          <-chan error
	commandWait         <-chan error
	transferWait        <-chan bool
}

func (coordinator *teardownCoordinator) run() {
	retryWait := deferredRetryWait
	for {
		ctx, cancel := context.WithTimeout(context.Background(), deferredShutdownWait)
		ready, err := coordinator.drain(ctx)
		if ready {
			if closed, closeErr := closeStorage(ctx, coordinator.runtime); closed {
				cancel()
				if closeErr != nil {
					log.Printf("deferred runtime storage closed with cleanup errors workspace=%s error=%v", coordinator.runtime.ID, closeErr)
				}
				return
			} else {
				err = errors.Join(err, closeErr)
			}
		}
		cancel()
		log.Printf("deferred workspace shutdown remains pending workspace=%s error=%v", coordinator.runtime.ID, err)
		time.Sleep(retryWait)
		if retryWait < deferredRetryMax/2 {
			retryWait *= 2
		} else {
			retryWait = deferredRetryMax
		}
	}
}

func (coordinator *teardownCoordinator) drain(ctx context.Context) (bool, error) {
	beginErr := coordinator.begin()
	actionErr := coordinator.drainActions(ctx)
	commandErr := coordinator.drainCommands(ctx)
	var sessionErr error
	if !coordinator.sessionsDrained {
		if sessions := coordinator.runtime.Connectors.ConsoleSessionManager(); sessions != nil {
			sessionErr = sessions.CloseAll(ctx)
		}
		coordinator.sessionsDrained = sessionErr == nil
	}
	transferErr := coordinator.drainTransfers(ctx)
	ready := coordinator.actionsRecovered && coordinator.commandsRecovered && coordinator.sessionsDrained && coordinator.transferRecovered
	return ready, errors.Join(beginErr, actionErr, commandErr, sessionErr, transferErr)
}

func (coordinator *teardownCoordinator) begin() error {
	var beginErrors []error
	if !coordinator.actionsResolved {
		coordinator.actionsResolved = coordinator.resolveActions == nil
		if coordinator.resolveActions != nil {
			workflow, err := coordinator.resolveActions()
			if err != nil {
				beginErrors = append(beginErrors, fmt.Errorf("initialize connector action shutdown: %w", err))
			} else {
				coordinator.actions, coordinator.actionsResolved = workflow, true
			}
		}
	}
	if coordinator.actionsResolved {
		if coordinator.actions == nil {
			coordinator.actionsDrained, coordinator.actionsRecovered = true, true
			clearActionIdentity(coordinator.runtime)
		} else {
			if !coordinator.actionsBegun {
				coordinator.actions.BeginShutdown()
				coordinator.actionsBegun = true
			}
			if coordinator.actionWait == nil {
				wait := make(chan error, 1)
				coordinator.actionWait = wait
				go func() { wait <- coordinator.actions.WaitShutdown(context.Background()) }()
			}
		}
	}
	if !coordinator.commandsResolved {
		coordinator.commandsResolved = coordinator.resolveCommands == nil
		if coordinator.resolveCommands != nil {
			workflow, err := coordinator.resolveCommands()
			if err != nil {
				beginErrors = append(beginErrors, fmt.Errorf("initialize command request shutdown: %w", err))
			} else {
				coordinator.commands, coordinator.commandsResolved = workflow, true
			}
		}
	}
	if coordinator.commandsResolved {
		if coordinator.commands == nil {
			coordinator.commandsDrained, coordinator.commandsRecovered = true, true
		} else {
			if !coordinator.commandsBegun {
				coordinator.commands.BeginWorkerShutdown()
				coordinator.commandsBegun = true
			}
			if coordinator.commandWait == nil {
				wait := make(chan error, 1)
				coordinator.commandWait = wait
				go func() { wait <- coordinator.commands.WaitWorkers(context.Background()) }()
			}
		}
	}
	if leases := coordinator.runtime.Security.VaultLeaseStore(); leases != nil {
		leases.Clear()
	}
	if sessions := coordinator.runtime.Connectors.ConsoleSessionManager(); sessions != nil {
		sessions.BeginCloseAll()
	} else {
		coordinator.sessionsDrained = true
	}
	if !coordinator.transferResolved {
		if coordinator.resolveTransfers != nil {
			coordinator.transfer = coordinator.resolveTransfers()
		}
		coordinator.transferResolved = true
		if coordinator.transfer == nil {
			coordinator.transferDrained, coordinator.transferRecovered = true, true
		}
	}
	if coordinator.transfer != nil && !coordinator.transferBegun {
		initialized, err := coordinator.transfer.BeginShutdown()
		if err != nil {
			beginErrors = append(beginErrors, fmt.Errorf("begin transfer shutdown: %w", err))
		} else {
			coordinator.transferBegun = true
			coordinator.transferInitialized = initialized
			if !initialized {
				coordinator.transferDrained, coordinator.transferRecovered = true, true
			} else {
				wait := make(chan bool, 1)
				coordinator.transferWait = wait
				go func() { wait <- coordinator.transfer.Wait(context.Background()) }()
			}
		}
	}
	return errors.Join(beginErrors...)
}

func (coordinator *teardownCoordinator) drainActions(ctx context.Context) error {
	if !coordinator.actionsResolved || coordinator.actions == nil {
		return nil
	}
	if !coordinator.actionsDrained {
		select {
		case err := <-coordinator.actionWait:
			if err != nil {
				coordinator.actionWait = nil
				return fmt.Errorf("stop connector action workers: %w", err)
			}
			coordinator.actionsDrained = true
		case <-ctx.Done():
			return fmt.Errorf("stop connector action workers: %w", ctx.Err())
		}
	}
	if !coordinator.actionsRecovered {
		if err := coordinator.actions.MarkRunningOutcomeUnknown(ctx, runtimeoutcome.ConnectorActionUnknown); err != nil {
			return fmt.Errorf("persist connector action shutdown: %w", err)
		}
		coordinator.actionsRecovered = true
		clearActionIdentity(coordinator.runtime)
	}
	return nil
}

func (coordinator *teardownCoordinator) drainCommands(ctx context.Context) error {
	if !coordinator.commandsResolved || coordinator.commands == nil {
		return nil
	}
	if !coordinator.commandsDrained {
		select {
		case err := <-coordinator.commandWait:
			if err != nil {
				coordinator.commandWait = nil
				return fmt.Errorf("stop command workers: %w", err)
			}
			coordinator.commandsDrained = true
		case <-ctx.Done():
			return fmt.Errorf("stop command workers: %w", ctx.Err())
		}
	}
	if !coordinator.commandsRecovered {
		if err := coordinator.commands.CancelRunning(ctx, runtimeoutcome.CommandCanceled); err != nil {
			return fmt.Errorf("persist command request shutdown: %w", err)
		}
		coordinator.commandsRecovered = true
	}
	return nil
}

func (coordinator *teardownCoordinator) drainTransfers(ctx context.Context) error {
	if !coordinator.transferResolved || coordinator.transfer == nil || coordinator.transferRecovered || !coordinator.transferBegun {
		return nil
	}
	if !coordinator.transferDrained {
		select {
		case coordinator.transferDrained = <-coordinator.transferWait:
			if !coordinator.transferDrained {
				return errors.New("transfer workers stopped without draining")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := coordinator.transfer.Recover(ctx, "interrupted by workspace shutdown", "queue stopped by workspace shutdown"); err != nil {
		return fmt.Errorf("recover transfer shutdown: %w", err)
	}
	coordinator.transferRecovered = true
	return nil
}

// Discard releases a partially opened runtime without running normal shutdown
// recovery against state that was never published.
func Discard(runtime *workspaceruntime.Runtime, resolveTransfers TransferWorkflowResolver, onComplete func()) error {
	if runtime == nil {
		if onComplete != nil {
			onComplete()
		}
		return nil
	}
	if resolveTransfers != nil {
		if transfer := resolveTransfers(); transfer != nil {
			transfer.Abort()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownWait)
	closed, err := closeStorage(ctx, runtime)
	cancel()
	if closed {
		if onComplete != nil {
			onComplete()
		}
		return err
	}
	runtime.StartTeardown(func() {
		retryDiscardStorage(runtime)
		if onComplete != nil {
			onComplete()
		}
	})
	return errors.Join(err, ErrShutdownDeferred)
}

func retryDiscardStorage(runtime *workspaceruntime.Runtime) {
	retryWait := deferredRetryWait
	for {
		ctx, cancel := context.WithTimeout(context.Background(), deferredShutdownWait)
		closed, err := closeStorage(ctx, runtime)
		cancel()
		if closed {
			if err != nil {
				log.Printf("discarded runtime storage closed with cleanup errors workspace=%s error=%v", runtime.ID, err)
			}
			return
		} else {
			log.Printf("discarded runtime storage close remains pending workspace=%s error=%v", runtime.ID, err)
		}
		time.Sleep(retryWait)
		if retryWait < deferredRetryMax/2 {
			retryWait *= 2
		} else {
			retryWait = deferredRetryMax
		}
	}
}

func clearActionIdentity(runtime *workspaceruntime.Runtime) {
	runtime.ClearActionIdentity()
}

func closeStorage(ctx context.Context, runtime *workspaceruntime.Runtime) (bool, error) {
	if dispatcher := runtime.Observation.AuditDispatcherService(); dispatcher != nil {
		if err := dispatcher.Stop(ctx); err != nil {
			return false, fmt.Errorf("stop audit dispatcher for runtime %q: %w", runtime.ID, err)
		}
	}
	clearActionIdentity(runtime)
	storage := &runtime.Storage
	var closeErrors []error
	if database := storage.DatabaseHandle(); database != nil {
		if err := database.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close encrypted database runtime %q: %w", runtime.ID, err))
			return false, errors.Join(closeErrors...)
		}
	}
	if ownership := storage.DatabaseOwnership(); ownership != nil {
		released, err := ownership.Release()
		if err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("release encrypted database runtime %q ownership: %w", runtime.ID, err))
		}
		if released {
			storage.ClearDatabaseOwnership()
		} else {
			return false, errors.Join(closeErrors...)
		}
	}
	return true, errors.Join(closeErrors...)
}
