package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	connectorHostPingDefaultAttempts = 4
	connectorHostPingTimeout         = 3 * time.Second
	connectorHostPingPause           = 150 * time.Millisecond
)

type connectorTargetHostPingRequest struct {
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Mode               string `json:"mode,omitempty"`
	TransportTargetRef string `json:"transport_target_ref,omitempty"`
	Attempts           int    `json:"attempts,omitempty"`
}

type connectorTargetHostPingAttempt struct {
	Attempt    int    `json:"attempt"`
	OK         bool   `json:"ok"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type connectorTargetHostPingResponse struct {
	OK                 bool                             `json:"ok"`
	Host               string                           `json:"host"`
	Port               int                              `json:"port"`
	Mode               string                           `json:"mode"`
	TransportTargetRef string                           `json:"transport_target_ref,omitempty"`
	Attempts           []connectorTargetHostPingAttempt `json:"attempts"`
	Sent               int                              `json:"sent"`
	Received           int                              `json:"received"`
	DurationMS         int64                            `json:"duration_ms"`
	Message            string                           `json:"message"`
}

func (s connectorTargetHandlers) pingConnectorTargetHost(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request connectorTargetHostPingRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.Host = strings.TrimSpace(request.Host)
	request.Mode = strings.TrimSpace(request.Mode)
	if request.Mode == "" {
		request.Mode = "direct"
	}
	request.TransportTargetRef = strings.TrimSpace(request.TransportTargetRef)
	if _, err := networkDialAddress(request.Host, request.Port); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch request.Mode {
	case "direct":
	case "over_ssh":
		if request.TransportTargetRef == "" {
			writeError(w, http.StatusBadRequest, "transport target ref is required for over_ssh")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "unsupported connection mode")
		return
	}
	attemptCount := request.Attempts
	if attemptCount <= 0 || attemptCount > connectorHostPingDefaultAttempts {
		attemptCount = connectorHostPingDefaultAttempts
	}

	transport := connectorNetworkTransport{server: s.Server, runtime: runtime}
	attempts := make([]connectorTargetHostPingAttempt, 0, connectorHostPingDefaultAttempts)
	started := time.Now()
	received := 0
	for i := 1; i <= attemptCount; i++ {
		attempt := connectorTargetHostPingAttempt{Attempt: i}
		attemptStarted := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), connectorHostPingTimeout)
		conn, err := transport.DialConnectorTCP(ctx, connectors.NetworkDialRequest{
			Mode:               request.Mode,
			Host:               request.Host,
			Port:               request.Port,
			TransportTargetRef: request.TransportTargetRef,
		})
		attempt.DurationMS = time.Since(attemptStarted).Milliseconds()
		cancel()
		if conn != nil {
			_ = conn.Close()
		}
		if err != nil {
			attempt.Error = s.redactForPersistence(r.Context(), runtime, normalizeNetworkPingError(err))
		} else {
			attempt.OK = true
			received++
		}
		attempts = append(attempts, attempt)
		if i < attemptCount {
			select {
			case <-r.Context().Done():
				writeError(w, http.StatusRequestTimeout, "ping canceled")
				return
			case <-time.After(connectorHostPingPause):
			}
		}
	}

	response := connectorTargetHostPingResponse{
		OK:                 received == attemptCount,
		Host:               request.Host,
		Port:               request.Port,
		Mode:               request.Mode,
		TransportTargetRef: request.TransportTargetRef,
		Attempts:           attempts,
		Sent:               attemptCount,
		Received:           received,
		DurationMS:         time.Since(started).Milliseconds(),
		Message:            connectorHostPingMessage(received, attemptCount),
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "connector.host.ping", map[string]any{
		"host":                 request.Host,
		"port":                 request.Port,
		"mode":                 request.Mode,
		"transport_target_ref": request.TransportTargetRef,
		"attempts":             attemptCount,
		"received":             received,
		"ok":                   response.OK,
	})
	writeJSON(w, http.StatusOK, response)
}

func normalizeNetworkPingError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	var dnsErr *net.DNSError
	if ok := errors.As(err, &dnsErr); ok && dnsErr.Name != "" {
		return "host lookup failed: " + dnsErr.Name
	}
	return message
}

func connectorHostPingMessage(received, sent int) string {
	if received == sent {
		return "Host and port are reachable."
	}
	if received == 0 {
		return "Host and port are not reachable from the selected connection mode."
	}
	return "Host and port are intermittently reachable."
}
