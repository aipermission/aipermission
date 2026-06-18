package api

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

const connectorNetworkDialTimeout = 12 * time.Second
const dockerHostInternalName = "host.docker.internal"

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
	return net.JoinHostPort(resolveConnectorDialHost(host), strconv.Itoa(port)), nil
}

func resolveConnectorDialHost(host string) string {
	if !strings.EqualFold(strings.TrimSpace(host), dockerHostInternalName) {
		return host
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if addresses, err := net.DefaultResolver.LookupHost(ctx, host); err == nil && len(addresses) > 0 {
		return host
	}
	if gateway, ok := linuxDefaultGatewayHost(); ok {
		return gateway
	}
	return host
}

func linuxDefaultGatewayHost() (string, bool) {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", false
	}
	return parseLinuxDefaultGatewayRoute(string(data))
}

func parseLinuxDefaultGatewayRoute(data string) (string, bool) {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[1] != "00000000" {
			continue
		}
		value, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil {
			continue
		}
		ip := net.IPv4(byte(value), byte(value>>8), byte(value>>16), byte(value>>24))
		if ip == nil || ip.Equal(net.IPv4zero) {
			continue
		}
		return ip.String(), true
	}
	return "", false
}
