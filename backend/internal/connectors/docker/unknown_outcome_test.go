package dockerconnector

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

var testExecMarker = regexp.MustCompile(`__AIPERMISSION_DOCKER_EXIT_[A-Z2-7]+__`)

func execReplyWithCompletion(t *testing.T, request connectors.CommandRunRequest, output string, exitCode int) connectors.CommandRunResult {
	t.Helper()
	marker := testExecMarker.FindString(request.Command)
	if marker == "" || !strings.Contains(request.Command, "sh -c ") {
		t.Fatalf("exec did not use an outer completion observer: %s", request.Command)
	}
	return connectors.CommandRunResult{Stdout: output + fmt.Sprintf("\n%s%d\n", marker, exitCode), ExitCode: exitCode, DispatchStarted: true}
}

type mutationReplyTransport struct {
	fakeCommandTransport
	mutation func(connectors.CommandRunRequest) (connectors.CommandRunResult, error)
	calls    int
}

func (transport *mutationReplyTransport) RunConnectorCommand(ctx context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	for _, operation := range []string{"exec", "start", "stop", "restart"} {
		if strings.Contains(request.Command, "docker "+operation+" ") {
			transport.calls++
			return transport.mutation(request)
		}
	}
	return transport.fakeCommandTransport.RunConnectorCommand(ctx, request)
}

func newMutationReplyTransport(mutation func(connectors.CommandRunRequest) (connectors.CommandRunResult, error)) *mutationReplyTransport {
	return &mutationReplyTransport{
		fakeCommandTransport: fakeCommandTransport{results: map[string]connectors.CommandRunResult{
			"docker ps -a --no-trunc --format '{{json .}}'": {Stdout: `{"ID":"111111111111","Names":"api","Image":"app:latest","State":"running","Status":"Up 1 hour"}`},
		}},
		mutation: mutation,
	}
}

func executePreparedMutation(t *testing.T, action string, transport connectors.CommandTransport) (connectors.ActionResult, error) {
	t.Helper()
	input := map[string]any{"container": "api"}
	if action == ActionContainerExec {
		input["command"] = "printf hi"
	}
	runtime := connectors.RuntimeContext{Target: dockerTarget(), Profile: dockerProfile("selected"), Capabilities: fakeCapabilities{transport: transport}}
	prepared, err := New().PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: action, Input: input, Reason: "Test Docker reply loss"})
	if err != nil {
		t.Fatal(err)
	}
	return New().ExecuteAction(t.Context(), runtime, prepared)
}

func TestDockerMutationsDoNotTreatCLIFailureAsProofOfNoEffect(t *testing.T) {
	for _, action := range []string{ActionContainerExec, ActionStartContainer, ActionStopContainer, ActionRestartContainer} {
		for _, stream := range []string{"stdout", "stderr"} {
			t.Run(action+"/"+stream, func(t *testing.T) {
				transport := newMutationReplyTransport(func(connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
					result := connectors.CommandRunResult{DispatchStarted: true, ExitCode: 1}
					if stream == "stdout" {
						result.Stdout = `Post "/containers/111111111111/restart": EOF`
					} else {
						result.Stderr = "connection reset while reading Docker response"
					}
					return result, nil
				})
				result, err := executePreparedMutation(t, action, transport)
				if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || connectors.ErrorDetails(err)["retry_safe"] != false || transport.calls != 1 {
					t.Fatalf("dispatched CLI failure = %#v, %v; calls=%d", result, err, transport.calls)
				}
				if result.Output != nil || result.DisplayText != "" {
					t.Fatalf("uncertain mutation exposed unconfirmed output: %#v", result)
				}
			})
		}
	}
}

func TestDockerMutationPreflightFailureDoesNotDispatch(t *testing.T) {
	transport := newMutationReplyTransport(func(connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
		t.Fatal("mutation dispatched after scope rejection")
		return connectors.CommandRunResult{}, nil
	})
	transport.results["docker ps -a --no-trunc --format '{{json .}}'"] = connectors.CommandRunResult{Stdout: `{"ID":"222222222222","Names":"other","State":"running"}`}
	_, err := executePreparedMutation(t, ActionRestartContainer, transport)
	if err == nil || connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown || transport.calls != 0 {
		t.Fatalf("preflight result = %v, calls=%d", err, transport.calls)
	}
}

func TestDockerMutationNoDispatchCLIRejectionRemainsFailure(t *testing.T) {
	for _, action := range []string{ActionContainerExec, ActionStartContainer, ActionStopContainer, ActionRestartContainer} {
		t.Run(action, func(t *testing.T) {
			transport := newMutationReplyTransport(func(connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
				return connectors.CommandRunResult{ExitCode: 127, Stderr: "cannot start transport"}, nil
			})
			result, err := executePreparedMutation(t, action, transport)
			if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown || (err == nil && result.Status != connectors.ResultFailed) {
				t.Fatalf("known no-dispatch failure = %#v, %v", result, err)
			}
		})
	}
}

func TestDockerLifecycleSuccessSurvivesRefreshObservationError(t *testing.T) {
	transport := newMutationReplyTransport(func(connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
		return connectors.CommandRunResult{DispatchStarted: true, Stdout: "api\n"}, nil
	})
	transport.sequences = map[string][]connectors.CommandRunResult{
		"docker ps -a --no-trunc --format '{{json .}}'": {transport.results["docker ps -a --no-trunc --format '{{json .}}'"], {ExitCode: 1, Stderr: "refresh failed"}},
	}
	result, err := executePreparedMutation(t, ActionStartContainer, transport)
	output, _ := result.Output.(map[string]any)
	if err != nil || result.Status != connectors.ResultCompleted || output["refresh_error"] == nil || transport.calls != 1 {
		t.Fatalf("confirmed lifecycle result = %#v, %v; calls=%d", result, err, transport.calls)
	}
}

func TestDockerExecObservedUserFailureIsNotClassifiedByOutputWords(t *testing.T) {
	transport := newMutationReplyTransport(func(request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
		return execReplyWithCompletion(t, request, "Post /containers/api/restart: EOF\n", 1), nil
	})
	result, err := executePreparedMutation(t, ActionContainerExec, transport)
	if err != nil || result.Status != connectors.ResultFailed || result.DisplayText != "Post /containers/api/restart: EOF\n" || transport.calls != 1 {
		t.Fatalf("observed command exit misclassified: %#v, %v", result, err)
	}
}

func TestDockerMutationTransportFailureBeforeDispatchRemainsOrdinary(t *testing.T) {
	cause := errors.New("transport admission canceled")
	for _, action := range []string{ActionContainerExec, ActionStartContainer, ActionStopContainer, ActionRestartContainer} {
		t.Run(action, func(t *testing.T) {
			transport := newMutationReplyTransport(func(connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
				return connectors.CommandRunResult{}, cause
			})
			_, err := executePreparedMutation(t, action, transport)
			if !errors.Is(err, cause) || connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
				t.Fatalf("predispatch transport result = %v", err)
			}
		})
	}
}
