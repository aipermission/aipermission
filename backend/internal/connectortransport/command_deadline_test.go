package connectortransport

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestNativeCommandDeadlineAndSingleDispatch(t *testing.T) {
	fixture := newNativeTransportFixture(t)
	cases := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{"default", 0, 30 * time.Second},
		{"negative-default", -1, 30 * time.Second},
		{"requested", 7, 7 * time.Second},
		{"maximum", 60, 60 * time.Second},
		{"above-maximum", 61, 60 * time.Second},
		{"largest-int", math.MaxInt, 60 * time.Second},
	}
	if strconv.IntSize == 64 {
		overflowSeconds := int64(18446744074)
		cases = append(cases, struct {
			name    string
			seconds int
			want    time.Duration
		}{"wrapped-positive-duration", int(overflowSeconds), 60 * time.Second})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var dispatched context.Context
			wantResult := connectors.CommandRunResult{Stdout: "partial", Stderr: "uncertain", ExitCode: 71, DurationMS: 17, DispatchStarted: true}
			wantErr := errors.New("outcome unknown after dispatch")
			started := time.Now()
			adapter := commandAdapterFunc(func(ctx context.Context, _ connectorapi.PeerIdentityGateway, _ connectorapi.LiveConsoleRuntime, ref, command string) (connectors.CommandRunResult, error) {
				calls++
				dispatched = ctx
				deadline, ok := ctx.Deadline()
				if !ok || deadline.Before(started.Add(test.want)) || deadline.After(time.Now().Add(test.want)) {
					t.Errorf("adapter deadline = %v, want %s from dispatch start", deadline, test.want)
				}
				if ref != fixture.carrier || command != "  fixture-command --literal  " {
					t.Errorf("dispatch changed literal input: %q %q", ref, command)
				}
				return wantResult, wantErr
			})
			transport := Command{Dependencies: Dependencies{Runtime: fixture.runtime, AdapterFor: func(kind string) connectorapi.Adapter {
				if kind != "carrier" {
					t.Errorf("adapter kind = %q", kind)
				}
				return adapter
			}}}
			result, err := transport.RunConnectorCommand(nil, connectors.CommandRunRequest{
				SourceTargetRef: fixture.source, TransportTargetRef: fixture.carrier,
				Command: "  fixture-command --literal  ", TimeoutSeconds: test.seconds,
			})
			if result != wantResult || err != wantErr || calls != 1 {
				t.Fatalf("single-dispatch result = %#v, error = %v, calls = %d", result, err, calls)
			}
			if dispatched.Err() != context.Canceled {
				t.Fatalf("command context not canceled after return: %v", dispatched.Err())
			}
			fixture.requireQuiescent(t)
		})
	}
}

func TestNativeCommandPreservesShorterCallerDeadlineAndValues(t *testing.T) {
	fixture := newNativeTransportFixture(t)
	type contextKey struct{}
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), contextKey{}, "caller-marker"), 5*time.Second)
	defer cancel()
	wantDeadline, _ := ctx.Deadline()
	calls := 0
	adapter := commandAdapterFunc(func(ctx context.Context, _ connectorapi.PeerIdentityGateway, _ connectorapi.LiveConsoleRuntime, _, _ string) (connectors.CommandRunResult, error) {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || !deadline.Equal(wantDeadline) || ctx.Value(contextKey{}) != "caller-marker" {
			t.Errorf("lost caller context: deadline=%v value=%v", deadline, ctx.Value(contextKey{}))
		}
		return connectors.CommandRunResult{DispatchStarted: true}, nil
	})
	transport := Command{Dependencies: Dependencies{Runtime: fixture.runtime, AdapterFor: func(string) connectorapi.Adapter { return adapter }}}
	result, err := transport.RunConnectorCommand(ctx, connectors.CommandRunRequest{
		SourceTargetRef: fixture.source, TransportTargetRef: fixture.carrier, Command: "fixture-command", TimeoutSeconds: 60,
	})
	if err != nil || !result.DispatchStarted || calls != 1 || ctx.Err() != nil {
		t.Fatalf("caller lifetime/result changed: result=%#v error=%v calls=%d caller=%v", result, err, calls, ctx.Err())
	}
	fixture.requireQuiescent(t)
}
