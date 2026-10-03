package conformance_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const protocolFixtureHost = "protocols"

type protocolCapabilities struct{ requestedHost string }

func (capabilities protocolCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.NetworkTransportCapabilityName {
		return capabilities
	}
	return nil
}

func (protocolCapabilities) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (capabilities protocolCapabilities) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	if request.SourceTargetRef != "mail:8:8" || request.Mode != "direct" || request.TransportTargetRef != "" || request.Host != capabilities.requestedHost {
		return nil, fmt.Errorf("mail fixture refused an unexpected transport identity")
	}
	switch request.Port {
	case 143, 993, 465, 587:
	default:
		return nil, fmt.Errorf("mail fixture refused an unexpected port")
	}
	return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", protocolFixtureHost, request.Port))
}

func requireProtocolFixture(t *testing.T) {
	t.Helper()
	requireConformance(t)
	if os.Getenv("AIPERMISSION_PROTOCOL_HOST") != protocolFixtureHost || os.Getenv("SSL_CERT_FILE") != "/fixture-material/ca.crt" {
		t.Fatal("protocol conformance requires the owned daemon and fixture-only CA")
	}
}
