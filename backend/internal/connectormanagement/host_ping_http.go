package connectormanagement

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const (
	hostPingDefaultAttempts = 4
	hostPingTimeout         = 3 * time.Second
	hostPingPause           = 150 * time.Millisecond
)

type HostPingRequest struct {
	ProjectID          int64  `json:"project_id"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Mode               string `json:"mode,omitempty"`
	TransportTargetRef string `json:"transport_target_ref,omitempty"`
	Attempts           int    `json:"attempts,omitempty"`
}

type HostPingAttempt struct {
	Attempt    int    `json:"attempt"`
	OK         bool   `json:"ok"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type HostPingResponse struct {
	OK                 bool              `json:"ok"`
	Host               string            `json:"host"`
	Port               int               `json:"port"`
	Mode               string            `json:"mode"`
	TransportTargetRef string            `json:"transport_target_ref,omitempty"`
	Attempts           []HostPingAttempt `json:"attempts"`
	Sent               int               `json:"sent"`
	Received           int               `json:"received"`
	DurationMS         int64             `json:"duration_ms"`
	Message            string            `json:"message"`
}

type HostPingScope struct {
	ValidateTransport func(context.Context, int64, string, string) error
	Probe             func(context.Context, connectors.NetworkDialRequest) error
	Redact            func(context.Context, string) string
	Observe           func(context.Context, string, map[string]any)
}

type HostPingScopeProvider func(http.ResponseWriter) (HostPingScope, bool)

type HostPingHTTPHandler struct{ scope HostPingScopeProvider }

func NewHostPingHTTPHandler(scope HostPingScopeProvider) *HostPingHTTPHandler {
	return &HostPingHTTPHandler{scope: scope}
}

func (h *HostPingHTTPHandler) Ping(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w)
	if !ok {
		return
	}
	var request HostPingRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	normalizeHostPingRequest(&request)
	if err := validateHostPingRequest(request); err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if connectors.UsesConnectorTransport(request.Mode, "direct") {
		if err := scope.ValidateTransport(r.Context(), request.ProjectID, request.Mode, request.TransportTargetRef); err != nil {
			httptransport.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	attemptCount := request.Attempts
	if attemptCount <= 0 || attemptCount > hostPingDefaultAttempts {
		attemptCount = hostPingDefaultAttempts
	}
	response, completed := runHostPingAttempts(r.Context(), scope, request, attemptCount)
	if !completed {
		httptransport.WriteError(w, http.StatusRequestTimeout, "ping canceled")
		return
	}
	scope.Observe(r.Context(), "connector.host.ping", map[string]any{
		"project_id": request.ProjectID, "host": request.Host, "port": request.Port,
		"mode": request.Mode, "transport_target_ref": request.TransportTargetRef,
		"attempts": attemptCount, "received": response.Received, "ok": response.OK,
	})
	httptransport.WriteJSON(w, http.StatusOK, response)
}

func (h *HostPingHTTPHandler) resolve(w http.ResponseWriter) (HostPingScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return HostPingScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return HostPingScope{}, false
	}
	if scope.ValidateTransport == nil || scope.Probe == nil || scope.Redact == nil || scope.Observe == nil {
		httptransport.WriteInternalError(w)
		return HostPingScope{}, false
	}
	return scope, true
}

func normalizeHostPingRequest(request *HostPingRequest) {
	request.Host = strings.TrimSpace(request.Host)
	request.Mode = strings.TrimSpace(request.Mode)
	if request.Mode == "" {
		request.Mode = "direct"
	}
	request.TransportTargetRef = strings.TrimSpace(request.TransportTargetRef)
}

func validateHostPingRequest(request HostPingRequest) error {
	if request.Host == "" {
		return errors.New("host is required")
	}
	if request.Port < 1 || request.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if connectors.UsesConnectorTransport(request.Mode, "direct") {
		if request.ProjectID < 1 {
			return errors.New("project_id is required for connector transport")
		}
		if request.TransportTargetRef == "" {
			return errors.New("transport target ref is required for connector transport")
		}
	}
	return nil
}

func runHostPingAttempts(ctx context.Context, scope HostPingScope, request HostPingRequest, count int) (HostPingResponse, bool) {
	attempts := make([]HostPingAttempt, 0, count)
	started := time.Now()
	received := 0
	for attemptNumber := 1; attemptNumber <= count; attemptNumber++ {
		attempt := HostPingAttempt{Attempt: attemptNumber}
		attemptStarted := time.Now()
		attemptContext, cancel := context.WithTimeout(ctx, hostPingTimeout)
		err := scope.Probe(attemptContext, connectors.NetworkDialRequest{
			SourceProjectID: request.ProjectID, Mode: request.Mode, Host: request.Host, Port: request.Port,
			TransportTargetRef: request.TransportTargetRef,
		})
		attempt.DurationMS = time.Since(attemptStarted).Milliseconds()
		cancel()
		if err != nil {
			attempt.Error = scope.Redact(ctx, normalizeHostPingError(err))
		} else {
			attempt.OK = true
			received++
		}
		attempts = append(attempts, attempt)
		if attemptNumber < count {
			select {
			case <-ctx.Done():
				return HostPingResponse{}, false
			case <-time.After(hostPingPause):
			}
		}
	}
	return HostPingResponse{
		OK: received == count, Host: request.Host, Port: request.Port, Mode: request.Mode,
		TransportTargetRef: request.TransportTargetRef, Attempts: attempts, Sent: count, Received: received,
		DurationMS: time.Since(started).Milliseconds(), Message: hostPingMessage(received, count),
	}, true
}

func normalizeHostPingError(err error) string {
	if err == nil {
		return ""
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.Name != "" {
		return "host lookup failed: " + dnsErr.Name
	}
	return err.Error()
}

func hostPingMessage(received, sent int) string {
	switch {
	case received == sent:
		return "Host and port are reachable."
	case received == 0:
		return "Host and port are not reachable from the selected connection mode."
	default:
		return "Host and port are intermittently reachable."
	}
}
