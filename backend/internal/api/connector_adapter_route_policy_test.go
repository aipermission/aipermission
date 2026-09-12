package api

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/api/httptransport"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestConnectorAdapterRoutePoliciesCrossTheTransportBoundary(t *testing.T) {
	tests := []struct {
		name string
		from connectorapi.RoutePolicy
		want httptransport.AdapterRoutePolicy
	}{
		{name: "UI read", from: connectorapi.RoutePolicyUIRead, want: httptransport.AdapterRoutePolicyUIRead},
		{name: "UI mutation", from: connectorapi.RoutePolicyUIMutation, want: httptransport.AdapterRoutePolicyUIMutation},
		{name: "unknown fails closed", from: connectorapi.RoutePolicy("future"), want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := connectorAdapterRoutePolicy(test.from); got != test.want {
				t.Fatalf("policy = %q, want %q", got, test.want)
			}
		})
	}
}
