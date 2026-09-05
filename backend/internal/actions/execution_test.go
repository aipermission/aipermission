package actions

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

type executionConnector struct {
	prepareConnector
	result connectors.ActionResult
	err    error
}

func (c *executionConnector) ExecuteAction(context.Context, connectors.RuntimeContext, connectors.PreparedAction) (connectors.ActionResult, error) {
	return c.result, c.err
}

func executionService(t *testing.T, connector connectors.Connector) *Service {
	t.Helper()
	registry := connectors.NewRegistry()
	if err := registry.Register(connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	return NewService(registry, &fakeResolver{})
}

func executionRequest(t *testing.T, kind string) ExecutionRequest {
	t.Helper()
	principal, err := executionprincipal.LocalOperator("workspace-test", "runtime-test")
	if err != nil {
		t.Fatalf("create principal: %v", err)
	}
	return ExecutionRequest{
		Prepared: PreparedRequest{
			Target: connectors.TargetView{ConnectorKind: kind},
			Action: connectors.PreparedAction{ConnectorKind: kind, ActionName: "query_readonly"},
		},
		Runtime: connectors.RuntimeContext{
			Principal: principal,
		},
	}
}

func TestServiceExecuteDispatchesValidResult(t *testing.T) {
	connector := &executionConnector{
		prepareConnector: prepareConnector{kind: "memory"},
		result:           connectors.ActionResult{Status: connectors.ResultCompleted, DisplayText: "done"},
	}
	result, err := executionService(t, connector).Execute(t.Context(), executionRequest(t, "memory"))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != connectors.ResultCompleted || result.DisplayText != "done" {
		t.Fatalf("result = %#v", result)
	}
}

func TestServiceExecuteRejectsInvalidPrincipalBeforeDispatch(t *testing.T) {
	connector := &executionConnector{prepareConnector: prepareConnector{kind: "memory"}}
	request := executionRequest(t, "memory")
	request.Runtime.Principal = executionprincipal.Principal{}
	if _, err := executionService(t, connector).Execute(t.Context(), request); err == nil {
		t.Fatal("expected invalid principal error")
	}
}

func TestServiceExecutePreservesConnectorError(t *testing.T) {
	want := errors.New("connector failed")
	connector := &executionConnector{prepareConnector: prepareConnector{kind: "memory"}, err: want}
	_, err := executionService(t, connector).Execute(t.Context(), executionRequest(t, "memory"))
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestServiceExecuteClassifiesMalformedResultsAsOutcomeUnknown(t *testing.T) {
	tests := []connectors.ActionResult{
		{Status: ""},
		{Status: connectors.ResultApprovalPending},
		{Status: connectors.ResultBlocked},
		{Status: connectors.ResultCanceled},
		{Status: connectors.ResultDeclined},
		{Status: connectors.ResultStale},
		{Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{SessionID: 7}},
		{Status: connectors.ResultCompleted, Handles: connectors.ActionHandles{SessionGeneration: 2}},
	}
	for _, result := range tests {
		connector := &executionConnector{prepareConnector: prepareConnector{kind: "memory"}, result: result}
		_, err := executionService(t, connector).Execute(t.Context(), executionRequest(t, "memory"))
		if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || connectors.ErrorCode(err) != "outcome_unknown" {
			t.Fatalf("result %#v error = %v status=%q code=%q", result, err, connectors.ErrorStatus(err), connectors.ErrorCode(err))
		}
		if details := connectors.ErrorDetails(err); details["dispatch_stage"] != "response_validation" || details["retry_safe"] != false {
			t.Fatalf("result %#v details = %#v", result, details)
		}
	}
}

func TestServiceExecuteAcceptsConnectorOwnedStatuses(t *testing.T) {
	for _, status := range []connectors.ResultStatus{
		connectors.ResultCompleted, connectors.ResultFailed, connectors.ResultError,
		connectors.ResultOutcomeUnknown, connectors.ResultRunning,
	} {
		connector := &executionConnector{
			prepareConnector: prepareConnector{kind: "memory"},
			result:           connectors.ActionResult{Status: status},
		}
		result, err := executionService(t, connector).Execute(t.Context(), executionRequest(t, "memory"))
		if err != nil {
			t.Fatalf("status %q rejected: %v", status, err)
		}
		if result.Status != status {
			t.Fatalf("status = %q, want %q", result.Status, status)
		}
	}
}
