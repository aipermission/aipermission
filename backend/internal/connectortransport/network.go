package connectortransport

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

const networkDialTimeout = 12 * time.Second
const dockerHostInternalName = "host.docker.internal"

type Network struct {
	Dependencies
	Approved Approved
}

func (Network) ConnectorRuntimeCapability() string { return connectors.NetworkTransportCapabilityName }

func (transport Network) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
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
	ctx, cancel := context.WithTimeout(ctx, networkDialTimeout)
	defer cancel()
	if !connectors.UsesConnectorTransport(mode, "direct") {
		return dialDirectConnectorTCP(ctx, request.Host, request.Port)
	}
	targetRef := strings.TrimSpace(request.TransportTargetRef)
	if targetRef == "" {
		return nil, fmt.Errorf("transport target ref is required for connection mode %q", mode)
	}
	kind, _, _, ok := connectors.ParseTargetRef(targetRef)
	if !ok {
		return nil, connectortargets.ErrInvalidTargetRef
	}
	if transport.Runtime.Database == nil {
		return nil, fmt.Errorf("database runtime is not available")
	}
	release, err := transport.Approved.Acquire(ctx, transport.Runtime, connectors.NetworkTransportCapabilityName, targetRef)
	if err != nil {
		return nil, err
	}
	defer release()
	store := connectortargets.NewStore(transport.Runtime.Database)
	var projectErr error
	if strings.TrimSpace(request.SourceTargetRef) != "" {
		projectErr = store.ValidateTransportTarget(ctx, request.SourceTargetRef, targetRef)
	} else if request.SourceProjectID > 0 {
		projectErr = store.ValidateTransportProject(ctx, request.SourceProjectID, targetRef)
	} else {
		return nil, fmt.Errorf("source target or project identity is required for connector transport")
	}
	if projectErr != nil {
		return nil, projectErr
	}
	var adapter connectorapi.TCPTransportAdapter
	if transport.AdapterFor != nil {
		adapter, _ = transport.AdapterFor(kind).(connectorapi.TCPTransportAdapter)
	}
	if adapter == nil {
		return nil, fmt.Errorf("%s connector does not expose TCP transport", kind)
	}
	return adapter.DialConnectorTCP(ctx, peerGateway{transport.TrustStorePath}, LiveRuntime(transport.Runtime, kind), targetRef, "tcp", address)
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

func dialDirectConnectorTCP(ctx context.Context, host string, port int) (net.Conn, error) {
	return dialDirectConnectorTCPWith(ctx, host, port, dialTCPAddress)
}

func dialDirectConnectorTCPWith(
	ctx context.Context,
	host string,
	port int,
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	host = resolveConnectorDialHost(strings.TrimSpace(host))
	address := net.JoinHostPort(host, strconv.Itoa(port))
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return dial(ctx, ipNetwork(ip), address)
	}
	// The standard TCP dialer owns DNS address ordering, per-address deadline
	// sharing, and IPv4/IPv6 fallback. A local serial loop can spend the whole
	// request deadline on the first unreachable address.
	return dial(ctx, "tcp", address)
}

func ipNetwork(ip net.IP) string {
	if ip.To4() != nil {
		return "tcp4"
	}
	return "tcp6"
}

func dialTCPAddress(ctx context.Context, network, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
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
		if ip != nil && !ip.Equal(net.IPv4zero) {
			return ip.String(), true
		}
	}
	return "", false
}
