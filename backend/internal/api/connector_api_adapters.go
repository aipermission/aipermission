package api

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/api/httptransport"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func connectorRuntimeCapabilitiesFor(kind string, server *Server, runtime *gatewayinfra.WorkspaceHandle) connectors.RuntimeCapabilityResolver {
	if server == nil || server.connectorRuntime == nil {
		return nil
	}
	return server.connectorRuntime.RuntimeCapabilities(runtime, kind)
}

func connectorAdapterRoutes(server *Server) []httptransport.AdapterRoute {
	connectorInfos := server.connectorRegistry().List()
	kinds := make([]string, 0, len(connectorInfos))
	for _, info := range connectorInfos {
		kinds = append(kinds, info.Kind)
	}
	routes, err := server.connectorRuntime.RouteDefinitions(kinds)
	if err != nil {
		panic(err)
	}
	registered := make([]httptransport.AdapterRoute, 0, len(routes))
	for _, route := range routes {
		handler := route.Handler
		registered = append(registered, httptransport.AdapterRoute{
			Method: route.Method,
			Path:   route.Path,
			Policy: connectorAdapterRoutePolicy(route.Policy),
			Handler: func(w http.ResponseWriter, r *http.Request) {
				handler(server.connectorRuntime.RouteGateway(), w, r)
			},
		})
	}
	return registered
}

func connectorAdapterRoutePolicy(policy connectorapi.RoutePolicy) httptransport.AdapterRoutePolicy {
	switch policy {
	case connectorapi.RoutePolicyUIRead:
		return httptransport.AdapterRoutePolicyUIRead
	case connectorapi.RoutePolicyUIMutation:
		return httptransport.AdapterRoutePolicyUIMutation
	default:
		return ""
	}
}
