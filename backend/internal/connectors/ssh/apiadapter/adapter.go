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
	maxConfigParseBytes      = 256 * 1024
)

type adapter struct{}

func init() {
	connectorapi.Register(sshconnector.Kind, adapter{})
}

func (a adapter) RegisterRoutes(mux connectorapi.RouteMux, server connectorapi.GatewayServer) {
	if mux == nil {
		return
	}
	mux.HandleFunc("POST /api/ssh-host-keys/approve", func(w http.ResponseWriter, r *http.Request) {
		a.approveHostKey(server, w, r)
	})
	mux.HandleFunc("GET /api/ssh-config/discover", func(w http.ResponseWriter, r *http.Request) {
		a.discoverConfig(server, w, r)
	})
	mux.HandleFunc("POST /api/ssh-config/parse", func(w http.ResponseWriter, r *http.Request) {
		a.parseConfig(server, w, r)
	})
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
