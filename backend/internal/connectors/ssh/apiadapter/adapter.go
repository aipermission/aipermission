// Package apiadapter registers the SSH connector's gateway adapter.
//
// The generic API package owns routing, auth, permission, approval, history,
// and audit. This package owns SSH-specific runtime behavior.
package apiadapter

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	vaultpkg "github.com/aipermission/aipermission/backend/internal/vault"
)

const (
	consoleConnectTimeout    = 15 * time.Second
	initialExecTimeout       = 3 * time.Second
	backgroundCommandTimeout = 30 * time.Minute
	finishRequestTimeout     = 10 * time.Second
	maxConfigParseBytes      = 256 * 1024
)

type adapter struct{}

func New() connectorapi.Adapter {
	return adapter{}
}

func (a adapter) Routes() []connectorapi.RouteDefinition {
	return []connectorapi.RouteDefinition{
		{Method: "POST", Path: "/api/ssh-host-keys/approve", Handler: a.approveHostKey},
		{Method: "GET", Path: "/api/ssh-config/discover", Handler: a.discoverConfig},
		{Method: "POST", Path: "/api/ssh-config/parse", Handler: a.parseConfig},
	}
}

func (adapter) RuntimeCapabilities(server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime) map[string]connectors.RuntimeCapability {
	return map[string]connectors.RuntimeCapability{
		sshconnector.RuntimeServiceName:             runtimeExecutor{server: server, runtime: runtime},
		connectors.SessionEnvironmentCapabilityName: sessionEnvironmentCapability{},
	}
}

func (adapter) RuntimeResources(database *sql.DB, secretVault *vaultpkg.Vault) map[string]any {
	if database == nil {
		return nil
	}
	if secretVault == nil {
		return nil
	}
	return map[string]any{
		"keys": sshkeys.NewStore(database, secretVault),
	}
}

func (adapter) WriteConnectorError(w http.ResponseWriter, err error) bool {
	if w == nil {
		return false
	}
	return writeUnknownHostKeyError(w, err)
}

func (adapter) ConnectorErrorMessage(prefix string, err error) string {
	switch strings.TrimSpace(prefix) {
	case "command execution failed":
		return commandFailureMessage(err)
	default:
		return connectionFailureMessage(err)
	}
}

func (adapter) LiveConsoleActionName() string {
	return sshconnector.ActionExec
}
