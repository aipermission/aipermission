package running

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type unknownConsoleSessions struct {
	connectorapi.ConsoleSessionRuntime
	err          error
	interrupts   int
	interruptErr error
	result       connectorapi.ConsoleExecResult
}

func (sessions *unknownConsoleSessions) WaitActive(context.Context, connectorapi.Principal, connectorapi.ConsoleSessionHandle) (connectorapi.ConsoleExecResult, error) {
	return sessions.result, sessions.err
}

func (sessions *unknownConsoleSessions) InterruptActive(context.Context, connectorapi.Principal, connectorapi.ConsoleSessionHandle) error {
	sessions.interrupts++
	return sessions.interruptErr
}

type unknownFinisher struct {
	status      connectors.ResultStatus
	output      any
	displayText string
	errorText   string
}

func (finisher *unknownFinisher) ConnectorFinishActionRequest(_ context.Context, _ int64, status connectors.ResultStatus, output any, displayText, errorText string, _ ...connectors.OutputHint) (connectorapi.ActionRequest, error) {
	finisher.status, finisher.output, finisher.displayText, finisher.errorText = status, output, displayText, errorText
	return connectorapi.ActionRequest{}, nil
}

func TestSSHBackgroundObservationFailuresRemainUnknown(t *testing.T) {
	for _, observeErr := range []error{context.DeadlineExceeded, context.Canceled, fmt.Errorf("observe: %w", console.ErrCommandOutcomeUnknown), errors.New("session disconnected")} {
		t.Run(observeErr.Error(), func(t *testing.T) {
			sessions := &unknownConsoleSessions{err: observeErr, interruptErr: errors.New("interrupt reply lost"), result: connectorapi.ConsoleExecResult{Output: "withheld-output-fixture"}}
			finisher := &unknownFinisher{}
			err := (Running{}).FinishRunning(t.Context(), finisher, runningActionRuntime{sessions: sessions}, 42,
				connectors.RuntimeActionContext{TargetRef: "ssh:1:2", TargetConnectorKind: "ssh", ActionName: "exec"},
				connectorapi.Principal{}, connectors.ActionHandles{SessionID: 7, SessionGeneration: 3})
			if err != nil || finisher.status != connectors.ResultOutcomeUnknown || finisher.displayText != "" {
				t.Fatalf("SSH background outcome = %#v, %v", finisher, err)
			}
			encoded, err := json.Marshal(finisher.output)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "withheld-output-fixture") || !strings.Contains(string(encoded), `"retry_safe":false`) || !strings.Contains(finisher.errorText, "inspect") {
				t.Fatalf("unsafe background outcome: output=%s error=%q", encoded, finisher.errorText)
			}
		})
	}
}

func TestSSHBackgroundResolutionFailuresRemainUnknown(t *testing.T) {
	for _, variant := range []string{"resolution", "console", "invalid-handle"} {
		t.Run(variant, func(t *testing.T) {
			runtime := runningActionRuntime{sessions: &unknownConsoleSessions{}}
			handles := connectors.ActionHandles{SessionID: 7, SessionGeneration: 3}
			switch variant {
			case "resolution":
				runtime.resolve = func(context.Context, string) (connectors.TargetView, connectors.CredentialProfileView, error) {
					return connectors.TargetView{}, connectors.CredentialProfileView{}, errors.New("profile removed")
				}
			case "console":
				runtime.sessions = nil
			case "invalid-handle":
				handles.SessionGeneration = 0
			}
			finisher := &unknownFinisher{}
			err := (Running{}).FinishRunning(t.Context(), finisher, runtime, 42,
				connectors.RuntimeActionContext{TargetRef: "ssh:1:2", TargetConnectorKind: "ssh", ActionName: "exec"}, connectorapi.Principal{}, handles)
			if err != nil || finisher.status != connectors.ResultOutcomeUnknown || !strings.Contains(finisher.errorText, "inspect") {
				t.Fatalf("lost dispatched resolution outcome: %#v, %v", finisher, err)
			}
		})
	}
}

func TestSSHBackgroundObservedExitKeepsCompletedOrFailed(t *testing.T) {
	for _, exitCode := range []int{0, 23} {
		t.Run(fmt.Sprintf("exit-%d", exitCode), func(t *testing.T) {
			sessions := &unknownConsoleSessions{result: connectorapi.ConsoleExecResult{SessionID: 7, Generation: 3, ExitCode: exitCode, Output: "observed-output"}}
			finisher := &unknownFinisher{}
			err := (Running{}).FinishRunning(t.Context(), finisher, runningActionRuntime{sessions: sessions}, 42,
				connectors.RuntimeActionContext{TargetRef: "ssh:1:2", TargetConnectorKind: "ssh", ActionName: "exec"},
				connectorapi.Principal{}, connectors.ActionHandles{SessionID: 7, SessionGeneration: 3})
			status := connectors.ResultCompleted
			if exitCode != 0 {
				status = connectors.ResultFailed
			}
			if err != nil || finisher.status != status || finisher.displayText != "observed-output" || finisher.errorText != "" || sessions.interrupts != 0 {
				t.Fatalf("observed result mislabeled: %#v, %v", finisher, err)
			}
		})
	}
}

func TestSSHSuccessfulInterruptDoesNotProveCompletion(t *testing.T) {
	sessions := &unknownConsoleSessions{err: context.DeadlineExceeded, result: connectorapi.ConsoleExecResult{Output: "withheld-output-fixture"}}
	finisher := &unknownFinisher{}
	err := (Running{}).FinishRunning(t.Context(), finisher, runningActionRuntime{sessions: sessions}, 42,
		connectors.RuntimeActionContext{TargetRef: "ssh:1:2", TargetConnectorKind: "ssh", ActionName: "exec"},
		connectorapi.Principal{}, connectors.ActionHandles{SessionID: 7, SessionGeneration: 3})
	if err != nil || finisher.status != connectors.ResultOutcomeUnknown || finisher.displayText != "" || sessions.interrupts != 1 || !strings.Contains(finisher.errorText, "does not confirm completion") {
		t.Fatalf("successful interrupt confirmed completion: %#v, interrupts=%d err=%v", finisher, sessions.interrupts, err)
	}
	encoded, err := json.Marshal(finisher.output)
	if err != nil || strings.Contains(string(encoded), "withheld-output-fixture") || !strings.Contains(string(encoded), `"retry_safe":false`) {
		t.Fatalf("unsafe interruption result: %s %v", encoded, err)
	}
}
