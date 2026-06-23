package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

const (
	defaultConnectorCommandTimeout = 30 * time.Second
	maxConnectorCommandTimeout     = 60 * time.Second
)

type connectorCommandTransport struct {
	server  *Server
	runtime *databaseRuntime
}

func (connectorCommandTransport) ConnectorRuntimeCapability() string {
	return connectors.CommandTransportCapabilityName
}

func (transport connectorCommandTransport) RunConnectorCommand(ctx context.Context, request connectors.CommandRunRequest) (connectors.CommandRunResult, error) {
	mode := strings.TrimSpace(request.Mode)
	if mode == "" {
		mode = "over_ssh"
	}
	if strings.TrimSpace(request.Command) == "" {
		return connectors.CommandRunResult{}, fmt.Errorf("command is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := defaultConnectorCommandTimeout
	if request.TimeoutSeconds > 0 {
		timeout = time.Duration(request.TimeoutSeconds) * time.Second
		if timeout > maxConnectorCommandTimeout {
			timeout = maxConnectorCommandTimeout
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	switch mode {
	case "over_ssh":
		targetRef := strings.TrimSpace(request.TransportTargetRef)
		if targetRef == "" {
			return connectors.CommandRunResult{}, fmt.Errorf("transport target ref is required for over_ssh")
		}
		kind, _, _, ok := connectortargets.ParseConnectorTargetRef(targetRef)
		if !ok {
			return connectors.CommandRunResult{}, connectortargets.ErrInvalidTargetRef
		}
		adapter, _ := connectorAPIAdapterFor(kind).(connectorapi.CommandTransportAdapter)
		if adapter == nil {
			return connectors.CommandRunResult{}, fmt.Errorf("%s connector does not expose command transport", kind)
		}
		return adapter.RunConnectorCommand(ctx, transport.server, transport.runtime, targetRef, request.Command)
	default:
		return connectors.CommandRunResult{}, fmt.Errorf("unsupported command transport mode %q", mode)
	}
}
