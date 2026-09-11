// Package management owns SSH connector configuration, credential, target,
// and runtime-material behavior exposed through the connector adapter.
package management

import (
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
)

const maxConfigParseBytes = 256 * 1024

type Management struct{}

func (m Management) Routes() []connectorapi.RouteDefinition {
	return []connectorapi.RouteDefinition{
		{Method: http.MethodPost, Path: "/api/ssh-host-keys/approve", Handler: m.approveHostKey},
		{Method: http.MethodGet, Path: "/api/ssh-config/discover", Handler: m.discoverConfig},
		{Method: http.MethodPost, Path: "/api/ssh-config/parse", Handler: m.parseConfig},
	}
}

func (Management) WriteConnectorError(w http.ResponseWriter, err error) bool {
	if w == nil {
		return false
	}
	return WriteUnknownHostKeyError(w, err)
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
