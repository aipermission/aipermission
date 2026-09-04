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
	server   *Server
	runtime  *databaseRuntime
	approved approvedConnectorTransports
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
		if transport.runtime == nil || transport.runtime.database == nil {
			return connectors.CommandRunResult{}, fmt.Errorf("database runtime is not available")
		}
		release, err := transport.approved.acquire(ctx, transport.runtime, connectors.CommandTransportCapabilityName, targetRef)
		if err != nil {
			return connectors.CommandRunResult{}, err
		}
		defer release()
		if err := connectortargets.NewStore(transport.runtime.database).ValidateTransportTarget(ctx, request.SourceTargetRef, targetRef); err != nil {
			return connectors.CommandRunResult{}, err
		}
		adapter, _ := transport.server.connectorAPIAdapterFor(kind).(connectorapi.CommandTransportAdapter)
		if adapter == nil {
			return connectors.CommandRunResult{}, fmt.Errorf("%s connector does not expose command transport", kind)
		}
		return adapter.RunConnectorCommand(ctx, transport.server, transport.runtime, targetRef, request.Command)
	default:
		return connectors.CommandRunResult{}, fmt.Errorf("unsupported command transport mode %q", mode)
	}
}
