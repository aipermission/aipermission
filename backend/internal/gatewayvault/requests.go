package gatewayvault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

var errRollbackAtomicVaultEffect = errors.New("roll back failed atomic Vault effect")

type requestMutationPort struct {
	component           *Component
	runtime             Runtime
	finalizationTimeout time.Duration
}

func (port requestMutationPort) executeAtomic(actions VaultActionApplication) vaultrequests.AtomicEffectExecutor {
	if port.component == nil || port.runtime.Requests.Transaction == nil {
		return nil
	}
	return func(ctx context.Context, request vaultrequests.Request, actor, userNote, finishedActionPrefix string) (vaultrequests.WorkflowResult, bool, error) {
		if request.ActionName != vaultrequests.ActionGenerateItem {
			return vaultrequests.WorkflowResult{}, false, nil
		}
		execution, handled, executeErr := actions.PrepareTransactional(ctx, request)
		if !handled {
			return vaultrequests.WorkflowResult{}, false, nil
		}
		if execution.Release != nil {
			defer execution.Release()
		}
		if executeErr != nil {
			result, err := port.completeAtomicFailure(ctx, actions, request, actor, userNote, finishedActionPrefix, executeErr)
			return result, handled, err
		}
		var result vaultrequests.WorkflowResult
		var effectErr error
		err := port.runtime.Requests.Transaction(ctx, func(tx *sql.Tx, appendObservation RequestObservationAppender) error {
			if execution.Run == nil {
				effectErr = vaultrequests.ErrRuntimeUnavailable
				return errRollbackAtomicVaultEffect
			}
			output, observations, runErr := execution.Run(ctx, tx)
			if runErr != nil {
				effectErr = runErr
				return errRollbackAtomicVaultEffect
			}
			completed, completeErr := vaultrequests.NewTxStore(tx).Complete(ctx, request.ID, vaultrequests.StatusCompleted, output, "", userNote)
			if completeErr != nil {
				effectErr = completeErr
				return errRollbackAtomicVaultEffect
			}
			for _, observation := range observations {
				if err := appendObservation(tx, "mcp", &request.TokenID, 0, observation.Action, observation.Payload); err != nil {
					effectErr = err
					return errRollbackAtomicVaultEffect
				}
			}
			if err := appendObservation(tx, actor, &request.TokenID, requestRuntimeID(request), finishedActionPrefix+"."+vaultrequests.StatusCompleted, vaultrequests.RequestAuditPayload(completed, userNote)); err != nil {
				effectErr = err
				return errRollbackAtomicVaultEffect
			}
			result = vaultrequests.WorkflowResult{Request: completed}
			return nil
		})
		if errors.Is(err, errRollbackAtomicVaultEffect) {
			result, err = port.completeAtomicFailure(ctx, actions, request, actor, userNote, finishedActionPrefix, effectErr)
		} else if err != nil {
			result, err = port.reconcileAtomicTransactionError(ctx, request, actor, userNote, finishedActionPrefix, err)
		}
		return result, handled, err
	}
}

func (port requestMutationPort) reconcileAtomicTransactionError(
	ctx context.Context,
	request vaultrequests.Request,
	actor, userNote, finishedActionPrefix string,
	transactionErr error,
) (vaultrequests.WorkflowResult, error) {
	timeout := port.finalizationTimeout
	if timeout <= 0 {
		timeout = vaultrequests.DefaultExecutionTimeout
	}
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	store := port.runtime.Requests.Store(finalizeCtx)
	if store == nil {
		return vaultrequests.WorkflowResult{}, transactionErr
	}
	current, err := store.Get(finalizeCtx, request.ID)
	if err != nil {
		return vaultrequests.WorkflowResult{}, fmt.Errorf("reconcile Vault transaction: %w; read request: %v", transactionErr, err)
	}
	if current.Status == vaultrequests.StatusCompleted {
		if err := port.runtime.Requests.RepairProjection(finalizeCtx, request.ID); err != nil {
			return vaultrequests.WorkflowResult{}, fmt.Errorf("reconcile committed Vault transaction: %w", err)
		}
		return vaultrequests.WorkflowResult{Request: current}, nil
	}
	if current.Status != vaultrequests.StatusRunning {
		return vaultrequests.WorkflowResult{Request: current, ExecutionError: transactionErr}, nil
	}
	errorText := port.runtime.Requests.RedactRequestError(finalizeCtx, transactionErr)
	failed, err := port.completeAuditedFailureWithMutation(
		finalizeCtx, request, actor, userNote, finishedActionPrefix,
		vaultrequests.StatusFailed, errorText,
	)
	if err != nil {
		return vaultrequests.WorkflowResult{}, fmt.Errorf("reconcile failed Vault transaction: %w", err)
	}
	if err := port.runtime.Requests.RepairProjection(finalizeCtx, request.ID); err != nil {
		return vaultrequests.WorkflowResult{}, fmt.Errorf("repair failed Vault transaction projection: %w", err)
	}
	return vaultrequests.WorkflowResult{Request: failed, ExecutionError: transactionErr}, nil
}

func (port requestMutationPort) completeAtomicFailure(
	ctx context.Context,
	actions VaultActionApplication,
	request vaultrequests.Request,
	actor, userNote, finishedActionPrefix string,
	executeErr error,
) (vaultrequests.WorkflowResult, error) {
	timeout := port.finalizationTimeout
	if timeout <= 0 {
		timeout = vaultrequests.DefaultExecutionTimeout
	}
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	status := vaultrequests.StatusFailed
	if actions.IsStale(executeErr) {
		status = vaultrequests.StatusStale
	}
	errorText := port.runtime.Requests.RedactRequestError(finalizeCtx, executeErr)
	var completed vaultrequests.Request
	err := port.runtime.Requests.Transaction(finalizeCtx, func(tx *sql.Tx, appendObservation RequestObservationAppender) error {
		var err error
		completed, err = vaultrequests.NewTxStore(tx).Complete(finalizeCtx, request.ID, status, nil, errorText, userNote)
		if err != nil {
			return err
		}
		return appendObservation(
			tx, actor, &request.TokenID, requestRuntimeID(request),
			finishedActionPrefix+"."+status, vaultrequests.RequestAuditPayload(completed, userNote),
		)
	})
	if err == nil {
		return vaultrequests.WorkflowResult{Request: completed, ExecutionError: executeErr}, nil
	}
	transactionErr := err
	store := port.runtime.Requests.Store(finalizeCtx)
	if store == nil {
		return vaultrequests.WorkflowResult{}, transactionErr
	}
	current, readErr := store.Get(finalizeCtx, request.ID)
	if readErr != nil {
		return vaultrequests.WorkflowResult{}, fmt.Errorf("reconcile Vault failure transaction: %w; read request: %v", transactionErr, readErr)
	}
	if current.Status == status {
		if repairErr := port.runtime.Requests.RepairProjection(finalizeCtx, request.ID); repairErr != nil {
			return vaultrequests.WorkflowResult{}, fmt.Errorf("repair committed Vault failure projection: %w", repairErr)
		}
		return vaultrequests.WorkflowResult{Request: current, ExecutionError: executeErr}, nil
	}
	if current.Status != vaultrequests.StatusRunning {
		return vaultrequests.WorkflowResult{Request: current, ExecutionError: executeErr}, nil
	}
	completed, err = port.completeAuditedFailureWithMutation(
		finalizeCtx, request, actor, userNote, finishedActionPrefix, status, errorText,
	)
	if err != nil {
		return vaultrequests.WorkflowResult{}, fmt.Errorf("reconcile Vault failure transaction: %w; terminalize request: %v", transactionErr, err)
	}
	if err := port.runtime.Requests.RepairProjection(finalizeCtx, request.ID); err != nil {
		return vaultrequests.WorkflowResult{}, fmt.Errorf("repair failed Vault transaction projection: %w", err)
	}
	return vaultrequests.WorkflowResult{Request: completed, ExecutionError: executeErr}, nil
}

func (port requestMutationPort) completeAuditedFailureWithMutation(
	ctx context.Context,
	request vaultrequests.Request,
	actor, userNote, finishedActionPrefix, status, errorText string,
) (vaultrequests.Request, error) {
	var completed vaultrequests.Request
	err := port.WithMutation(
		ctx, actor, &request.TokenID, requestRuntimeID(request), finishedActionPrefix+"."+status,
		func() any { return vaultrequests.RequestAuditPayload(completed, userNote) },
		func(tx *sql.Tx) error {
			var err error
			completed, err = vaultrequests.NewTxStore(tx).Complete(ctx, request.ID, status, nil, errorText, userNote)
			return err
		},
	)
	if err == nil {
		return completed, nil
	}
	store := port.runtime.Requests.Store(ctx)
	if store == nil {
		return vaultrequests.Request{}, err
	}
	current, readErr := store.Get(ctx, request.ID)
	if readErr != nil {
		return vaultrequests.Request{}, fmt.Errorf("reconcile audited Vault failure mutation: %w; read request: %v", err, readErr)
	}
	if current.Status != status {
		return vaultrequests.Request{}, err
	}
	if repairErr := port.runtime.Requests.RepairProjection(ctx, request.ID); repairErr != nil {
		return vaultrequests.Request{}, fmt.Errorf("repair committed Vault failure mutation projection: %w", repairErr)
	}
	return current, nil
}

func requestRuntimeID(request vaultrequests.Request) int64 {
	if request.RuntimeID == nil {
		return 0
	}
	return *request.RuntimeID
}

func (port requestMutationPort) WithMutation(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
	if port.component == nil || port.runtime.Requests.Mutate == nil {
		return vaultrequests.ErrRuntimeUnavailable
	}
	return port.runtime.Requests.Mutate(ctx, actor, tokenID, runtimeID, action, payload, mutate)
}

func (port requestMutationPort) Observe(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	if port.component != nil && port.runtime.Requests.Observe != nil {
		port.runtime.Requests.Observe(ctx, actor, tokenID, runtimeID, action, payload)
	}
}

func (component *Component) RequestRuntime(ctx context.Context, runtime Runtime) (VaultRequestApplication, error) {
	if component == nil || runtime.Storage.Database == nil || runtime.Storage.DatabaseID == "" || runtime.Requests.Store == nil || component.dependencies.AllowRequest == nil ||
		runtime.Requests.Transaction == nil || runtime.Requests.Mutate == nil || runtime.Requests.RepairProjection == nil ||
		runtime.Requests.RedactRequestError == nil || runtime.Requests.RedactRequestValue == nil ||
		runtime.Requests.SealRequest == nil || runtime.Requests.OpenRequest == nil || runtime.Storage.SecretVault == nil ||
		runtime.Storage.WorkspaceID == "" || runtime.Session.MCPStarted == nil || runtime.Session.AcquireDelivery == nil {
		return nil, vaultrequests.ErrRuntimeUnavailable
	}
	actions, err := component.ActionRuntime(runtime)
	if err != nil {
		return nil, err
	}
	executionTimeout := runtime.Requests.ExecutionTimeout
	if executionTimeout <= 0 {
		executionTimeout = vaultrequests.DefaultExecutionTimeout
	}
	mutations := requestMutationPort{component: component, runtime: runtime, finalizationTimeout: executionTimeout}
	owner, err := vaultrequests.NewRuntime(vaultrequests.RuntimeDependencies{
		Store: runtime.Requests.Store(ctx), Mutations: mutations,
		Prepare: actions.Prepare, AuthorizeOutput: actions.AuthorizeOutput,
		AllowRequest: func(tokenID int64) bool {
			return component.dependencies.AllowRequest("vault-request:" + runtime.Storage.DatabaseID + ":" + strconv.FormatInt(tokenID, 10))
		},
		Execute: actions.Execute, ExecuteAtomic: mutations.executeAtomic(actions), Compensate: actions.Compensate,
		RepairProjection: func(ctx context.Context, id int64) error {
			return runtime.Requests.RepairProjection(ctx, id)
		},
		RedactError:      func(ctx context.Context, err error) string { return runtime.Requests.RedactRequestError(ctx, err) },
		RedactProjection: runtime.Requests.RedactRequestValue,
		SealRequest: func(id int64, envelope vaultrequests.ExecutionEnvelope) (string, error) {
			return runtime.Requests.SealRequest(id, envelope)
		},
		OpenRequest: func(id int64, sealed string) (vaultrequests.ExecutionEnvelope, error) {
			var envelope vaultrequests.ExecutionEnvelope
			err := runtime.Requests.OpenRequest(id, sealed, &envelope)
			return envelope, err
		},
		IsStale: actions.IsStale, MCPStarted: runtime.Session.MCPStarted,
		AcquireDelivery: runtime.Session.AcquireDelivery, ExecutionTimeout: executionTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Vault request runtime: %w", err)
	}
	return owner, nil
}
