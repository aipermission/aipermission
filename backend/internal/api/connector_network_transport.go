package api

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

const connectorNetworkDialTimeout = 12 * time.Second

type connectorNetworkTransport struct {
	server  *Server
	runtime *databaseRuntime
}

func (connectorNetworkTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (transport connectorNetworkTransport) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	mode := strings.TrimSpace(request.Mode)
	if mode == "" {
		mode = "direct"
	}
	address, err := networkDialAddress(request.Host, request.Port)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, connectorNetworkDialTimeout)
	defer cancel()
	switch mode {
	case "direct":
		var dialer net.Dialer
		return dialer.DialContext(ctx, "tcp", address)
	case "over_ssh":
		targetRef := strings.TrimSpace(request.TransportTargetRef)
		if targetRef == "" {
			return nil, fmt.Errorf("transport target ref is required for over_ssh")
		}
		kind, _, _, ok := connectortargets.ParseConnectorTargetRef(targetRef)
		if !ok {
			return nil, connectortargets.ErrInvalidTargetRef
		}
		adapter, _ := connectorAPIAdapterFor(kind).(connectorapi.TCPTransportAdapter)
		if adapter == nil {
			return nil, fmt.Errorf("%s connector does not expose TCP transport", kind)
		}
		return adapter.DialConnectorTCP(ctx, transport.server, transport.runtime, targetRef, "tcp", address)
	default:
		return nil, fmt.Errorf("unsupported connection mode %q", mode)
	}
}

func networkDialAddress(host string, port int) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("host is required")
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("port must be between 1 and 65535")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}
