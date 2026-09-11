package connectortransport

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

const defaultCommandTimeout = 30 * time.Second
const MaxCommandTimeout = 60 * time.Second

type Command struct {
	Dependencies
	Approved Approved
}

func (Command) ConnectorRuntimeCapability() string { return connectors.CommandTransportCapabilityName }

func (transport Command) RunConnectorCommand(ctx context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	mode := strings.TrimSpace(request.Mode)
	if mode == "" {
		mode = "connector"
	}
	if strings.TrimSpace(request.Command) == "" {
		return connectors.CommandRunResult{}, fmt.Errorf("command is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := defaultCommandTimeout
	if request.TimeoutSeconds > 0 {
		timeout = time.Duration(request.TimeoutSeconds) * time.Second
		if timeout > MaxCommandTimeout {
			timeout = MaxCommandTimeout
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	targetRef := strings.TrimSpace(request.TransportTargetRef)
	if targetRef == "" {
		return connectors.CommandRunResult{}, fmt.Errorf("transport target ref is required for command mode %q", mode)
	}
	kind, _, _, ok := connectors.ParseTargetRef(targetRef)
	if !ok {
		return connectors.CommandRunResult{}, connectortargets.ErrInvalidTargetRef
	}
	if transport.Runtime.Database == nil {
		return connectors.CommandRunResult{}, fmt.Errorf("database runtime is not available")
	}
	release, err := transport.Approved.Acquire(ctx, transport.Runtime, connectors.CommandTransportCapabilityName, targetRef)
	if err != nil {
		return connectors.CommandRunResult{}, err
	}
	defer release()
	if err := connectortargets.NewStore(transport.Runtime.Database).ValidateTransportTarget(ctx, request.SourceTargetRef, targetRef); err != nil {
		return connectors.CommandRunResult{}, err
	}
	var adapter connectorapi.CommandTransportAdapter
	if transport.AdapterFor != nil {
		adapter, _ = transport.AdapterFor(kind).(connectorapi.CommandTransportAdapter)
	}
	if adapter == nil {
		return connectors.CommandRunResult{}, fmt.Errorf("%s connector does not expose command transport", kind)
	}
	return adapter.RunConnectorCommand(ctx, peerGateway{transport.TrustStorePath}, LiveRuntime(transport.Runtime, kind), targetRef, request.Command)
}
