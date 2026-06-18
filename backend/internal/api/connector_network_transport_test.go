package api

import "testing"

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
