// Package management owns SSH connector configuration, credential, target,
// and runtime-material behavior exposed through the connector adapter.
package management

import (
	"net/http"
	"strings"

	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

const maxConfigParseBytes = 256 * 1024

type Management struct{}

func (m Management) Routes() []connectorapi.RouteDefinition {
	return []connectorapi.RouteDefinition{
		{Method: http.MethodPost, Path: "/api/ssh-host-keys/approve", Policy: connectorapi.RoutePolicyUIMutation, Handler: m.approveHostKey},
		{Method: http.MethodGet, Path: "/api/ssh-config/discover", Policy: connectorapi.RoutePolicyUIRead, Handler: m.discoverConfig},
		{Method: http.MethodPost, Path: "/api/ssh-config/parse", Policy: connectorapi.RoutePolicyUIMutation, Handler: m.parseConfig},
	}
}

func (Management) PresentConnectorError(err error) (connectorapi.ErrorPresentation, bool) {
	return PresentUnknownHostKeyError(err)
}

func (Management) ConnectorErrorMessage(prefix string, err error) string {
	switch strings.TrimSpace(prefix) {
	case "command execution failed":
		return CommandFailureMessage(err)
	default:
		return ConnectionFailureMessage(err)
	}
}

func (Management) LiveConsoleActionName() string { return sshconnector.ActionExec }
