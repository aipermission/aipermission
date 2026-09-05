package apiadapter

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshconfig"
)

func (adapter) approveHostKey(server connectorapi.RouteGateway, w http.ResponseWriter, r *http.Request) {
	gateway, err := routeGatewayFrom(server)
	if err != nil {
		writeInternalError(w)
		return
	}
	var input hostKeyApprovalRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	input.Host = strings.TrimSpace(input.Host)
	if input.Port == 0 {
		input.Port = 22
	}
	if input.Host == "" {
		writeError(w, http.StatusBadRequest, "host is required")
		return
	}
	if err := validateHost(input.Host); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Port < 1 || input.Port > 65535 {
		writeError(w, http.StatusBadRequest, "port must be between 1 and 65535")
		return
	}
	if strings.TrimSpace(input.PublicKey) == "" {
		writeError(w, http.StatusBadRequest, "public_key is required")
		return
	}
	hostname := net.JoinHostPort(input.Host, strconv.Itoa(input.Port))
	key, err := execution.ParseHostPublicKey(input.PublicKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Replace {
		err = gateway.ConnectorChangeVaultPeerTrust(r.Context(), func() error {
			return execution.ReplaceHostKey(gateway.ConnectorTrustStorePath(), hostname, input.PublicKey)
		})
	} else {
		err = execution.TrustHostKey(gateway.ConnectorTrustStorePath(), hostname, input.PublicKey)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, hostKeyApprovalResponse{
		Status:            "approved",
		Hostname:          hostname,
		KeyType:           key.Type(),
		FingerprintSHA256: execution.HostKeyFingerprintSHA256(key),
	})
}

func (adapter) discoverConfig(server connectorapi.RouteGateway, w http.ResponseWriter, _ *http.Request) {
	gateway, err := routeGatewayFrom(server)
	if err != nil {
		writeInternalError(w)
		return
	}
	if !gateway.ConnectorActiveRuntimeAvailable(w) {
		return
	}
	entries, err := sshconfig.DiscoverDefault()
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read gateway ssh config")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": entries})
}

func (adapter) parseConfig(server connectorapi.RouteGateway, w http.ResponseWriter, r *http.Request) {
	gateway, err := routeGatewayFrom(server)
	if err != nil {
		writeInternalError(w)
		return
	}
	if !gateway.ConnectorActiveRuntimeAvailable(w) {
		return
	}
	var input parseConfigRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	input.Content = strings.TrimSpace(input.Content)
	if input.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if len([]byte(input.Content)) > maxConfigParseBytes {
		writeError(w, http.StatusBadRequest, "content is too large")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": sshconfig.Parse(input.Content)})
}
