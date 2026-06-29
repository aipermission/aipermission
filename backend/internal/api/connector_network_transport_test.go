package api

import (
	"net"
	"testing"
)

func TestParseLinuxDefaultGatewayRoute(t *testing.T) {
	gateway, ok := parseLinuxDefaultGatewayRoute(`Iface	Destination	Gateway	Flags	RefCnt	Use	Metric	Mask
eth0	00000000	010011AC	0003	0	0	0	00000000
`)
	if !ok {
		t.Fatalf("expected default gateway")
	}
	if gateway != "172.17.0.1" {
		t.Fatalf("gateway = %q", gateway)
	}
}

func TestParseLinuxDefaultGatewayRouteRejectsMissingDefault(t *testing.T) {
	if gateway, ok := parseLinuxDefaultGatewayRoute(`Iface	Destination	Gateway	Flags	RefCnt	Use	Metric	Mask
eth0	0008A8C0	00000000	0001	0	0	0	00FFFFFF
`); ok || gateway != "" {
		t.Fatalf("unexpected gateway %q ok=%v", gateway, ok)
	}
}

func TestPreferredDialAddressesPreferIPv4(t *testing.T) {
	addrs := preferredDialAddresses([]net.IPAddr{
		{IP: net.ParseIP("2001:db8::10")},
		{IP: net.ParseIP("192.0.2.10")},
	}, 443)
	if len(addrs) != 2 {
		t.Fatalf("addresses = %#v", addrs)
	}
	if addrs[0].network != "tcp4" || addrs[0].address != "192.0.2.10:443" {
		t.Fatalf("first address = %#v", addrs[0])
	}
	if addrs[1].network != "tcp6" || addrs[1].address != "[2001:db8::10]:443" {
		t.Fatalf("second address = %#v", addrs[1])
	}
}
