package vaultrequests

import (
	"context"
	"errors"
	"testing"
)

func workflowPorts(state *workflowState) WorkflowPorts {
	return WorkflowPorts{
		Claim: func(context.Context, int64) (Request, error) {
			state.claimed++
			return Request{ID: 7, Status: StatusRunning}, state.claimErr
		},
		Execute: func(context.Context, Request) (any, error) {
			state.executed++
			return state.output, state.executeErr
		},
		Complete: func(_ context.Context, _ int64, status string, _ any, message string) (Request, error) {
			state.completions = append(state.completions, status+":"+message)
			if len(state.completions) == 1 && state.completeErr != nil {
				return Request{}, state.completeErr
			}
			return Request{ID: 7, Status: status, Error: message}, nil
		},
		Get:         func(context.Context, int64) (Request, error) { return state.current, state.getErr },
		Repair:      func(context.Context, int64) error { state.repaired++; return state.repairErr },
		Compensate:  func(context.Context, Request, any) error { state.compensated++; return state.compensateErr },
		RedactError: func(context.Context, error) string { return "redacted execution error" },
		IsStale:     func(err error) bool { return errors.Is(err, errWorkflowStale) },
	}
}

var errWorkflowStale = errors.New("stale")

type workflowState struct {
	claimed, executed, repaired, compensated                            int
	completions                                                         []string
	output                                                              any
	claimErr, executeErr, completeErr, getErr, repairErr, compensateErr error
	current                                                             Request
}

func TestRunWorkflowCompletesSuccessfulEffect(t *testing.T) {
	state := &workflowState{output: map[string]any{"ok": true}}
	result, err := RunWorkflow(t.Context(), 7, workflowPorts(state))
	if err != nil || result.Request.Status != StatusCompleted || result.ExecutionError != nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if state.claimed != 1 || state.executed != 1 || state.compensated != 0 {
		t.Fatalf("state=%#v", state)
	}
}

func TestRunClaimedWorkflowDoesNotClaimAlwaysRequestAgain(t *testing.T) {
	state := &workflowState{}
	result, err := RunClaimedWorkflow(t.Context(), Request{ID: 7, Status: StatusRunning}, workflowPorts(state))
	if err != nil || result.Request.Status != StatusCompleted {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if state.claimed != 0 || state.executed != 1 {
		t.Fatalf("state=%#v", state)
	}
}

func TestRunWorkflowPersistsStaleExecution(t *testing.T) {
	state := &workflowState{executeErr: errWorkflowStale}
	result, err := RunWorkflow(t.Context(), 7, workflowPorts(state))
	if err != nil || result.Request.Status != StatusStale || !errors.Is(result.ExecutionError, errWorkflowStale) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if state.completions[0] != StatusStale+":redacted execution error" {
		t.Fatalf("completions=%#v", state.completions)
	}
}

func TestRunWorkflowRepairsAlreadyFinalizedRequest(t *testing.T) {
	state := &workflowState{completeErr: errors.New("reply lost"), current: Request{ID: 7, Status: StatusCompleted}}
	result, err := RunWorkflow(t.Context(), 7, workflowPorts(state))
	if err != nil || result.Request.Status != StatusCompleted || state.repaired != 1 || state.compensated != 0 {
		t.Fatalf("result=%#v state=%#v err=%v", result, state, err)
	}
}

func TestRunWorkflowCompensatesUnfinalizedEffect(t *testing.T) {
	state := &workflowState{completeErr: errors.New("commit failed"), getErr: ErrNotFound}
	result, err := RunWorkflow(t.Context(), 7, workflowPorts(state))
	if err != nil || result.Request.Status != StatusFailed || state.compensated != 1 {
		t.Fatalf("result=%#v state=%#v err=%v", result, state, err)
	}
	if len(state.completions) != 2 {
		t.Fatalf("completions=%#v", state.completions)
	}
}

func TestRunWorkflowFailsWhenCompensationFails(t *testing.T) {
	state := &workflowState{completeErr: errors.New("commit failed"), getErr: ErrNotFound, compensateErr: errors.New("cleanup failed")}
	if _, err := RunWorkflow(t.Context(), 7, workflowPorts(state)); err == nil || state.compensated != 1 {
		t.Fatalf("state=%#v err=%v", state, err)
	}
}
