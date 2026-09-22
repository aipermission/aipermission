package connectortransport

import (
	"context"
	"net"
	"testing"
)

func TestParseLinuxDefaultGatewayRoute(t *testing.T) {
	gateway, ok := parseLinuxDefaultGatewayRoute(`Iface	Destination	Gateway	Flags	RefCnt	Use	Metric	Mask
eth0	00000000	010011AC	0003	0	0	0	00000000
`)
	if !ok || gateway != "172.17.0.1" {
		t.Fatalf("gateway=%q ok=%v", gateway, ok)
	}
}

func TestParseLinuxDefaultGatewayRouteRejectsMissingDefault(t *testing.T) {
	if gateway, ok := parseLinuxDefaultGatewayRoute(`Iface	Destination	Gateway	Flags	RefCnt	Use	Metric	Mask
eth0	0008A8C0	00000000	0001	0	0	0	00FFFFFF
`); ok || gateway != "" {
		t.Fatalf("unexpected gateway %q ok=%v", gateway, ok)
	}
}

func TestDirectConnectorDialDelegatesHostnameFallbackToStandardDialer(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		host        string
		wantNetwork string
		wantAddress string
	}{
		{name: "hostname", host: "database.example.test", wantNetwork: "tcp", wantAddress: "database.example.test:443"},
		{name: "ipv4 literal", host: "192.0.2.10", wantNetwork: "tcp4", wantAddress: "192.0.2.10:443"},
		{name: "ipv6 literal", host: "2001:db8::10", wantNetwork: "tcp6", wantAddress: "[2001:db8::10]:443"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			calls := 0
			_, err := dialDirectConnectorTCPWith(t.Context(), testCase.host, 443, func(ctx context.Context, network, address string) (net.Conn, error) {
				calls++
				if ctx != t.Context() || network != testCase.wantNetwork || address != testCase.wantAddress {
					t.Fatalf("dial ctx=%v network=%q address=%q", ctx, network, address)
				}
				return nil, nil
			})
			if err != nil || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}
