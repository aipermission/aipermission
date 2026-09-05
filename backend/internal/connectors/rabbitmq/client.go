package rabbitmqconnector

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type rabbitClient struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

func newRabbitClient(ctx context.Context, runtime connectors.RuntimeContext) (*rabbitClient, error) {
	transport, _ := runtime.Capability(connectors.NetworkTransportCapabilityName).(connectors.NetworkTransport)
	if transport == nil {
		return nil, ErrMissingTransport
	}
	username := strings.TrimSpace(stringValue(runtime.Profile.Public, "username"))
	if username == "" {
		return nil, fmt.Errorf("%w: username is required", ErrMissingSecret)
	}
	password, err := runtime.Secrets.GetSecret(ctx, "password")
	if errors.Is(err, connectors.ErrSecretNotFound) {
		return nil, fmt.Errorf("%w: password is required", ErrMissingSecret)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: resolve rabbitmq password: %w", connectors.ErrSecretProvider, err)
	}
	if strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("%w: password is required", ErrMissingSecret)
	}
	basicCredential := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	connectors.RegisterSensitiveValue(runtime.Secrets, basicCredential, "Basic "+basicCredential)
	scheme := rabbitScheme(runtime.Target)
	host := rabbitHost(runtime.Target)
	port := rabbitPort(runtime.Target)
	request := connectors.NetworkDialRequest{
		SourceTargetRef:    runtime.Target.Ref,
		SourceProjectID:    runtime.Target.ProjectID,
		Mode:               connectionMode(runtime.Target),
		Host:               host,
		Port:               port,
		TransportTargetRef: strings.TrimSpace(stringValue(runtime.Target.Config, "transport_target_ref")),
	}
	return &rabbitClient{
		baseURL:    fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(host, strconv.Itoa(port))),
		username:   username,
		password:   password,
		httpClient: connectors.NewHTTPClient(transport, request, rabbitHTTPTimeout),
	}, nil
}

func (client *rabbitClient) Get(ctx context.Context, path string, out any) error {
	_, _, err := client.do(ctx, http.MethodGet, path, nil, out)
	return err
}

func (client *rabbitClient) Post(ctx context.Context, path string, payload any, out any) error {
	dispatched, definiteResponse, err := client.do(ctx, http.MethodPost, path, payload, out)
	if err == nil || !dispatched || definiteResponse {
		return err
	}
	return &rabbitPostDispatchError{err: err}
}

func (client *rabbitClient) do(ctx context.Context, method string, path string, payload any, out any) (dispatched bool, definiteResponse bool, err error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return false, false, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
	if err != nil {
		return false, false, err
	}
	req.SetBasicAuth(client.username, client.password)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req, requestDispatched := connectors.TrackHTTPRequestDispatch(req)
	resp, err := client.httpClient.Do(req)
	if err != nil {
		return requestDispatched(), false, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRabbitHTTPBodyBytes+1))
	if err != nil {
		return true, false, err
	}
	if len(data) > maxRabbitHTTPBodyBytes {
		return true, false, fmt.Errorf("rabbitmq response is larger than %d bytes", maxRabbitHTTPBodyBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		definiteResponse := resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode < http.StatusInternalServerError
		return true, definiteResponse, rabbitHTTPError(resp.StatusCode, data)
	}
	if out == nil {
		return true, true, nil
	}
	if len(data) == 0 {
		return true, false, fmt.Errorf("rabbitmq response body is empty")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return true, false, fmt.Errorf("decode rabbitmq response: %w", err)
	}
	return true, true, nil
}

type rabbitPostDispatchError struct{ err error }

func (e *rabbitPostDispatchError) Error() string { return e.err.Error() }
func (e *rabbitPostDispatchError) Unwrap() error { return e.err }

func classifyRabbitMutationError(operation string, err error) error {
	var dispatchErr *rabbitPostDispatchError
	if !errors.As(err, &dispatchErr) {
		return err
	}
	return connectors.ClassifyOutcomeUnknown(
		"management_api_request",
		nil,
		fmt.Errorf("RabbitMQ %s outcome is unknown after dispatch: %w", operation, err),
	)
}

func rabbitHTTPError(status int, data []byte) error {
	message := strings.TrimSpace(string(data))
	if len(message) > 800 {
		message = message[:800] + "...[truncated]"
	}
	if message == "" {
		message = http.StatusText(status)
	}
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("rabbitmq authentication failed: %s", message)
	case http.StatusForbidden:
		return fmt.Errorf("rabbitmq permission denied: %s", message)
	case http.StatusNotFound:
		return fmt.Errorf("rabbitmq resource not found: %s", message)
	default:
		return fmt.Errorf("rabbitmq management API returned %d: %s", status, message)
	}
}

func classifyRabbitTestError(err error) connectors.TestStatus {
	if err == nil {
		return connectors.TestOK
	}
	if errors.Is(err, connectors.ErrSecretProvider) {
		return connectors.TestUnknownError
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "authentication"), strings.Contains(message, "unauthorized"), strings.Contains(message, "password"):
		return connectors.TestFailedAuth
	case strings.Contains(message, "permission denied"), strings.Contains(message, "forbidden"):
		return connectors.TestFailedPermission
	case strings.Contains(message, "tls"), strings.Contains(message, "certificate"):
		return connectors.TestFailedTLS
	case strings.Contains(message, "connection refused"), strings.Contains(message, "i/o timeout"), strings.Contains(message, "no such host"), strings.Contains(message, "network"), strings.Contains(message, "http/0.9"), strings.Contains(message, "malformed http"), strings.Contains(message, "server gave http response"):
		return connectors.TestFailedNetwork
	default:
		return connectors.TestUnknownError
	}
}

func rabbitSummary(output map[string]any) string {
	product := strings.TrimSpace(fmt.Sprint(output["product_name"]))
	version := strings.TrimSpace(fmt.Sprint(output["rabbitmq_version"]))
	if product == "" && version == "" {
		return "RabbitMQ overview read."
	}
	return strings.TrimSpace(product + " " + version)
}

func slimQueue(row map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{
		"name", "vhost", "durable", "auto_delete", "exclusive", "state", "messages", "messages_ready", "messages_unacknowledged", "consumers", "memory", "idle_since",
	} {
		if value, ok := row[key]; ok {
			out[key] = value
		}
	}
	return out
}

func queueListDisplay(rows []map[string]any) string {
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("%s ready=%v unacked=%v consumers=%v", row["name"], row["messages_ready"], row["messages_unacknowledged"], row["consumers"]))
	}
	return strings.Join(lines, "\n")
}

func queueDetailDisplay(row map[string]any) string {
	return fmt.Sprintf("%s ready=%v unacked=%v consumers=%v state=%v", row["name"], row["messages_ready"], row["messages_unacknowledged"], row["consumers"], row["state"])
}

func connectionMode(target connectors.TargetView) string {
	mode := strings.TrimSpace(stringValue(target.Config, "connection_mode"))
	if mode == "" {
		return "direct"
	}
	return mode
}

func rabbitScheme(target connectors.TargetView) string {
	scheme := strings.ToLower(strings.TrimSpace(stringValue(target.Config, "scheme")))
	switch scheme {
	case "https":
		return "https"
	case "auto":
		if connectors.UseVerifiedTLSByDefault(connectionMode(target), rabbitHost(target)) {
			return "https"
		}
		return "http"
	default:
		return defaultRabbitMQScheme
	}
}

func rabbitHost(target connectors.TargetView) string {
	host := strings.TrimSpace(stringValue(target.Config, "host"))
	if host == "" {
		return defaultRabbitMQHost
	}
	return host
}

func rabbitPort(target connectors.TargetView) int {
	return normalizeInt(target.Config, "port", defaultRabbitMQPort, 1, 65535)
}

func rabbitVHost(target connectors.TargetView) string {
	return normalizeVHost(target.Config, "vhost", defaultRabbitMQVHost)
}

func normalizeVHost(input map[string]any, key string, fallback string) string {
	value := strings.TrimSpace(stringValue(input, key))
	if value == "" {
		value = fallback
	}
	if value == "" {
		return defaultRabbitMQVHost
	}
	return value
}

func pathPart(value string) string {
	return url.PathEscape(value)
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value := values[key]
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func normalizeInt(values map[string]any, key string, fallback int, minValue int, maxValue int) int {
	if values == nil {
		return fallback
	}
	value, ok := values[key]
	if !ok || value == nil || value == "" {
		return fallback
	}
	var parsed int
	switch typed := value.(type) {
	case int:
		parsed = typed
	case int64:
		parsed = int(typed)
	case float64:
		parsed = int(typed)
	case json.Number:
		n, err := typed.Int64()
		if err != nil {
			return fallback
		}
		parsed = int(n)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return fallback
		}
		parsed = n
	default:
		return fallback
	}
	if parsed < minValue {
		return minValue
	}
	if parsed > maxValue {
		return maxValue
	}
	return parsed
}

func normalizeJSONMap(values map[string]any, key string) (map[string]any, error) {
	if values == nil {
		return map[string]any{}, nil
	}
	value, ok := values[key]
	if !ok || value == nil || value == "" {
		return map[string]any{}, nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed, nil
	case string:
		var decoded map[string]any
		if err := json.Unmarshal([]byte(typed), &decoded); err != nil {
			return nil, fmt.Errorf("%s must be a JSON object", key)
		}
		if decoded == nil {
			return map[string]any{}, nil
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("%s must be a JSON object", key)
	}
}

func copyMap(input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func truncateString(value string, maxBytes int) string {
	if maxBytes < 1 || len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "...[truncated]"
}

func min(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

var _ connectors.Connector = Connector{}
var _ connectors.TestableConnector = Connector{}
