package gatewayvault

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/aipermission/aipermission/backend/internal/vaultactions"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type requestMutationPort struct {
	component *Component
	runtime   Runtime
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
		var result vaultrequests.WorkflowResult
		err := port.runtime.Requests.Transaction(ctx, func(tx *sql.Tx, appendObservation RequestObservationAppender) error {
			var output any
			var observations []vaultactions.TransactionalObservation
			if executeErr == nil {
				if execution.Run == nil {
					return vaultrequests.ErrRuntimeUnavailable
				}
				var runErr error
				output, observations, runErr = execution.Run(ctx, tx)
				executeErr = runErr
			}
			status := vaultrequests.StatusCompleted
			errorText := ""
			if executeErr != nil {
				status = vaultrequests.StatusFailed
				errorText = port.runtime.Requests.RedactRequestError(ctx, executeErr)
				if actions.IsStale(executeErr) {
					status = vaultrequests.StatusStale
				}
			}
			completed, completeErr := vaultrequests.NewTxStore(tx).Complete(ctx, request.ID, status, output, errorText, userNote)
			if completeErr != nil {
				return completeErr
			}
			for _, observation := range observations {
				if err := appendObservation(tx, "mcp", &request.TokenID, 0, observation.Action, observation.Payload); err != nil {
					return err
				}
			}
			if err := appendObservation(tx, actor, &request.TokenID, requestRuntimeID(request), finishedActionPrefix+"."+status, vaultrequests.RequestAuditPayload(completed, userNote)); err != nil {
				return err
			}
			result = vaultrequests.WorkflowResult{Request: completed, ExecutionError: executeErr}
			return nil
		})
		return result, handled, err
	}
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
		runtime.Requests.Transaction == nil || runtime.Requests.RepairProjection == nil || runtime.Requests.RedactRequestError == nil || runtime.Session.MCPStarted == nil {
		return nil, vaultrequests.ErrRuntimeUnavailable
	}
	actions, err := component.ActionRuntime(runtime)
	if err != nil {
		return nil, err
	}
	owner, err := vaultrequests.NewRuntime(vaultrequests.RuntimeDependencies{
		Store: runtime.Requests.Store(ctx), Mutations: requestMutationPort{component: component, runtime: runtime},
		Prepare: actions.Prepare, AuthorizeOutput: actions.AuthorizeOutput,
		AllowRequest: func(tokenID int64) bool {
			return component.dependencies.AllowRequest("vault-request:" + runtime.Storage.DatabaseID + ":" + strconv.FormatInt(tokenID, 10))
		},
		Execute: actions.Execute, ExecuteAtomic: requestMutationPort{component: component, runtime: runtime}.executeAtomic(actions), Compensate: actions.Compensate,
		RepairProjection: func(ctx context.Context, id int64) error {
			return runtime.Requests.RepairProjection(ctx, id)
		},
		RedactError: func(ctx context.Context, err error) string { return runtime.Requests.RedactRequestError(ctx, err) },
		IsStale:     actions.IsStale, MCPStarted: runtime.Session.MCPStarted,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Vault request runtime: %w", err)
	}
	return owner, nil
}
