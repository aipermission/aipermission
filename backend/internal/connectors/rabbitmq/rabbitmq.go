// Package rabbitmqconnector defines the RabbitMQ connector contract.
package rabbitmqconnector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	Kind    = "rabbitmq"
	Label   = "RabbitMQ"
	Version = "0.2"

	ActionOverview     = "overview"
	ActionListVhosts   = "list_vhosts"
	ActionListQueues   = "list_queues"
	ActionGetQueue     = "get_queue"
	ActionListBindings = "list_bindings"
	ActionPeekMessages = "peek_messages"
	ActionPublish      = "publish_message"

	defaultRabbitMQScheme = "http"
	defaultRabbitMQHost   = "127.0.0.1"
	defaultRabbitMQPort   = 15672
	defaultRabbitMQVHost  = "/"

	defaultQueueLimit      = 250
	maxQueueLimit          = 1000
	defaultPeekCount       = 5
	maxPeekCount           = 50
	defaultPayloadMaxBytes = 64 << 10
	maxPayloadBytes        = 256 << 10
	maxPublishPayloadBytes = 256 << 10
	maxRabbitHTTPBodyBytes = 1 << 20
	maxRabbitReasonBytes   = 2000
	rabbitHTTPTimeout      = 15 * time.Second
)

var (
	ErrUnsupportedAction = errors.New("unsupported rabbitmq connector action")
	ErrMissingTransport  = errors.New("rabbitmq connector network transport is unavailable")
	ErrMissingSecret     = errors.New("rabbitmq connector credential is missing required secret")
	ErrInvalidConfig     = errors.New("rabbitmq connector target config is invalid")
)

// Connector describes RabbitMQ as a connector-shaped target for bounded queue
// inspection through the RabbitMQ Management API.
type Connector struct{}

func New() Connector {
	return Connector{}
}

func (Connector) Kind() string {
	return Kind
}

func (Connector) Label() string {
	return Label
}

func (Connector) Version() string {
	return Version
}

func (Connector) TargetSchema() connectors.Schema {
	return connectors.Schema{Fields: []connectors.Field{
		{
			Name:        "connection_mode",
			Label:       "Connection mode",
			Type:        connectors.FieldSelect,
			Required:    true,
			Default:     "direct",
			Description: "Connect directly from the local gateway, or tunnel to the RabbitMQ Management API through an SSH connector profile.",
			Options: []connectors.FieldOption{
				{Value: "direct", Label: "Direct"},
				{Value: "over_ssh", Label: "Over SSH"},
			},
		},
		{
			Name:        "scheme",
			Label:       "Scheme",
			Type:        connectors.FieldSelect,
			Required:    true,
			Default:     defaultRabbitMQScheme,
			Description: "RabbitMQ Management API HTTP scheme.",
			Options: []connectors.FieldOption{
				{Value: "http", Label: "HTTP"},
				{Value: "https", Label: "HTTPS"},
			},
		},
		{
			Name:        "host",
			Label:       "Host",
			Type:        connectors.FieldString,
			Required:    true,
			Default:     defaultRabbitMQHost,
			Description: "RabbitMQ Management API host as seen by the selected connection mode. For Over SSH this is usually 127.0.0.1 on the remote server.",
		},
		{
			Name:        "port",
			Label:       "Management API port",
			Type:        connectors.FieldNumber,
			Required:    true,
			Default:     defaultRabbitMQPort,
			Description: "RabbitMQ Management API TCP port, usually 15672 or the port shown in the management URL. Do not use the AMQP listener port.",
		},
		{
			Name:        "vhost",
			Label:       "Default vhost",
			Type:        connectors.FieldString,
			Default:     defaultRabbitMQVHost,
			Description: "Default RabbitMQ virtual host for queue actions.",
		},
		{
			Name:        "transport_target_ref",
			Label:       "SSH transport target",
			Type:        connectors.FieldString,
			Description: "Connector target ref used when connection_mode is over_ssh.",
		},
	}}
}

func (Connector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{
		{
			Kind:        "username_password",
			Label:       "Username and password",
			Description: "RabbitMQ Management API username and password stored through the encrypted vault layer.",
			Schema: connectors.Schema{Fields: []connectors.Field{
				{
					Name:        "username",
					Label:       "Username",
					Type:        connectors.FieldString,
					Required:    true,
					Description: "RabbitMQ Management API username.",
				},
				{
					Name:        "password",
					Label:       "Password",
					Type:        connectors.FieldSecret,
					Required:    true,
					Secret:      true,
					Description: "RabbitMQ Management API password.",
				},
			}},
		},
	}
}

func (Connector) GetHelp(_ context.Context, target connectors.TargetView) (connectors.ConnectorHelp, error) {
	title := "RabbitMQ target"
	if strings.TrimSpace(target.Name) != "" {
		title = "RabbitMQ target: " + target.Name
	}
	return connectors.ConnectorHelp{
		Title:       title,
		Summary:     "Inspect RabbitMQ vhosts, queues, bindings, and bounded message previews through AIPermission approval rules.",
		Connector:   Label,
		ConnectorID: Kind,
		Usage: []string{
			"Use list_queues before reading queue details when the queue name is unknown.",
			"Use get_queue to inspect one queue's metadata and counters.",
			"Use peek_messages only when the operator approved payload inspection; messages are requested with ack_requeue_true.",
			"Use publish_message only for intentional message creation; it is a write action and should normally start in Prompt mode.",
			"RabbitMQ destructive actions such as purge, ack, and delete are intentionally not part of the 0.2.6 MVP.",
		},
		Warnings: []string{
			"RabbitMQ message payloads may contain secrets or customer data. Redaction is best-effort; avoid reading payloads unless explicitly approved.",
			"peek_messages uses the Management API get endpoint with ack_requeue_true and bounded count/truncate limits.",
			"publish_message creates a new message in RabbitMQ. Use a dedicated credential with RabbitMQ-level write scope.",
			"RabbitMQ credential profiles decide what the RabbitMQ server itself allows.",
		},
	}, nil
}

func (Connector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{
		{
			Name:        ActionOverview,
			Label:       "Overview",
			Description: "Read bounded RabbitMQ overview metadata.",
			Category:    "metadata",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{},
			OutputHint:  connectors.OutputHint{Format: "json", MaxBytes: 128 << 10},
		},
		{
			Name:        ActionListVhosts,
			Label:       "List vhosts",
			Description: "List RabbitMQ virtual hosts visible to this credential.",
			Category:    "metadata",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{},
			OutputHint:  connectors.OutputHint{Format: "json", MaxRows: 500},
		},
		{
			Name:        ActionListQueues,
			Label:       "List queues",
			Description: "List queues in one virtual host with bounded rows.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "vhost", Label: "Vhost", Type: connectors.FieldString, Description: "Optional vhost; defaults to target vhost."},
				{Name: "pattern", Label: "Pattern", Type: connectors.FieldString, Description: "Optional case-insensitive queue name filter."},
				{Name: "limit", Label: "Limit", Type: connectors.FieldNumber, Default: defaultQueueLimit},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: maxQueueLimit},
		},
		{
			Name:        ActionGetQueue,
			Label:       "Read queue",
			Description: "Read metadata and counters for one RabbitMQ queue.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "vhost", Label: "Vhost", Type: connectors.FieldString, Description: "Optional vhost; defaults to target vhost."},
				{Name: "queue", Label: "Queue", Type: connectors.FieldString, Required: true},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 128 << 10},
		},
		{
			Name:        ActionListBindings,
			Label:       "List bindings",
			Description: "List bindings for one vhost or one queue.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "vhost", Label: "Vhost", Type: connectors.FieldString, Description: "Optional vhost; defaults to target vhost."},
				{Name: "queue", Label: "Queue", Type: connectors.FieldString, Description: "Optional queue name."},
				{Name: "limit", Label: "Limit", Type: connectors.FieldNumber, Default: defaultQueueLimit},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: maxQueueLimit},
		},
		{
			Name:        ActionPeekMessages,
			Label:       "Peek messages",
			Description: "Read a bounded preview of queue messages with ack_requeue_true.",
			Category:    "browser",
			Risk:        connectors.RiskRead,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "vhost", Label: "Vhost", Type: connectors.FieldString, Description: "Optional vhost; defaults to target vhost."},
				{Name: "queue", Label: "Queue", Type: connectors.FieldString, Required: true},
				{Name: "count", Label: "Count", Type: connectors.FieldNumber, Default: defaultPeekCount},
				{Name: "max_payload_bytes", Label: "Max payload bytes", Type: connectors.FieldNumber, Default: defaultPayloadMaxBytes},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: maxPayloadBytes},
		},
		{
			Name:        ActionPublish,
			Label:       "Publish message",
			Description: "Publish one bounded message through the RabbitMQ Management API.",
			Category:    "write",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "vhost", Label: "Vhost", Type: connectors.FieldString, Description: "Optional vhost; defaults to target vhost."},
				{Name: "exchange", Label: "Exchange", Type: connectors.FieldString, Default: "amq.default", Description: "Exchange name. Use amq.default to route directly to a queue by routing key."},
				{Name: "routing_key", Label: "Routing key", Type: connectors.FieldString, Required: true},
				{Name: "payload", Label: "Payload", Type: connectors.FieldMultiline, Required: true},
				{Name: "payload_encoding", Label: "Payload encoding", Type: connectors.FieldSelect, Default: "string", Options: []connectors.FieldOption{
					{Value: "string", Label: "String"},
					{Value: "base64", Label: "Base64"},
				}},
				{Name: "properties", Label: "Properties", Type: connectors.FieldJSON, Description: "Optional AMQP properties JSON object."},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 4000},
		},
	}, nil
}

func (Connector) PrepareAction(_ context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	input := copyMap(req.Input)
	risk := connectors.RiskRead
	title := ""
	summary := ""
	vhost := rabbitVHost(req.Target)
	switch req.ActionName {
	case ActionOverview:
		title = "Read RabbitMQ overview"
		summary = "Read bounded RabbitMQ overview metadata."
	case ActionListVhosts:
		title = "List RabbitMQ vhosts"
		summary = "List visible RabbitMQ virtual hosts."
	case ActionListQueues:
		vhost = normalizeVHost(input, "vhost", vhost)
		pattern := strings.TrimSpace(stringValue(input, "pattern"))
		limit := normalizeInt(input, "limit", defaultQueueLimit, 1, maxQueueLimit)
		input["vhost"] = vhost
		input["pattern"] = pattern
		input["limit"] = limit
		title = "List RabbitMQ queues"
		summary = fmt.Sprintf("List queues in vhost %q.", vhost)
	case ActionGetQueue:
		vhost = normalizeVHost(input, "vhost", vhost)
		queue := strings.TrimSpace(stringValue(input, "queue"))
		if queue == "" {
			return connectors.PreparedAction{}, fmt.Errorf("queue is required")
		}
		input["vhost"] = vhost
		input["queue"] = queue
		title = "Read RabbitMQ queue"
		summary = fmt.Sprintf("%s/%s", vhost, queue)
	case ActionListBindings:
		vhost = normalizeVHost(input, "vhost", vhost)
		queue := strings.TrimSpace(stringValue(input, "queue"))
		limit := normalizeInt(input, "limit", defaultQueueLimit, 1, maxQueueLimit)
		input["vhost"] = vhost
		input["queue"] = queue
		input["limit"] = limit
		title = "List RabbitMQ bindings"
		if queue != "" {
			summary = fmt.Sprintf("List bindings for %s/%s.", vhost, queue)
		} else {
			summary = fmt.Sprintf("List bindings in vhost %q.", vhost)
		}
	case ActionPeekMessages:
		vhost = normalizeVHost(input, "vhost", vhost)
		queue := strings.TrimSpace(stringValue(input, "queue"))
		if queue == "" {
			return connectors.PreparedAction{}, fmt.Errorf("queue is required")
		}
		count := normalizeInt(input, "count", defaultPeekCount, 1, maxPeekCount)
		maxBytes := normalizeInt(input, "max_payload_bytes", defaultPayloadMaxBytes, 1, maxPayloadBytes)
		input["vhost"] = vhost
		input["queue"] = queue
		input["count"] = count
		input["max_payload_bytes"] = maxBytes
		title = "Peek RabbitMQ messages"
		summary = fmt.Sprintf("Peek %d message(s) from %s/%s with requeue.", count, vhost, queue)
	case ActionPublish:
		risk = connectors.RiskWrite
		vhost = normalizeVHost(input, "vhost", vhost)
		exchange := strings.TrimSpace(stringValue(input, "exchange"))
		if exchange == "" {
			exchange = "amq.default"
		}
		routingKey := strings.TrimSpace(stringValue(input, "routing_key"))
		if routingKey == "" {
			return connectors.PreparedAction{}, fmt.Errorf("routing_key is required")
		}
		payload := stringValue(input, "payload")
		if payload == "" {
			return connectors.PreparedAction{}, fmt.Errorf("payload is required")
		}
		if len(payload) > maxPublishPayloadBytes {
			return connectors.PreparedAction{}, fmt.Errorf("payload is larger than %d bytes", maxPublishPayloadBytes)
		}
		encoding := strings.ToLower(strings.TrimSpace(stringValue(input, "payload_encoding")))
		if encoding == "" {
			encoding = "string"
		}
		if encoding != "string" && encoding != "base64" {
			return connectors.PreparedAction{}, fmt.Errorf("payload_encoding must be string or base64")
		}
		properties, err := normalizeJSONMap(input, "properties")
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		input["vhost"] = vhost
		input["exchange"] = exchange
		input["routing_key"] = routingKey
		input["payload"] = payload
		input["payload_encoding"] = encoding
		input["properties"] = properties
		title = "Publish RabbitMQ message"
		summary = fmt.Sprintf("Publish one message to %s/%s using routing key %q.", vhost, exchange, routingKey)
	default:
		return connectors.PreparedAction{}, ErrUnsupportedAction
	}
	if len(req.Reason) > maxRabbitReasonBytes {
		return connectors.PreparedAction{}, fmt.Errorf("reason is too large")
	}
	return connectors.PreparedAction{
		ConnectorKind: Kind,
		TargetRef:     req.Target.Ref,
		ProfileID:     req.Profile.ID,
		ActionName:    req.ActionName,
		Risk:          risk,
		Title:         title,
		Summary:       summary,
		Preview:       input,
		Payload:       input,
		ContextMaterial: map[string]any{
			"target":          req.Target.Name,
			"profile":         req.Profile.Label,
			"connection_mode": connectionMode(req.Target),
			"vhost":           vhost,
		},
	}, nil
}

func (Connector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	client, err := newRabbitClient(ctx, runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	switch action.ActionName {
	case ActionOverview:
		return executeOverview(ctx, client)
	case ActionListVhosts:
		return executeListVhosts(ctx, client)
	case ActionListQueues:
		return executeListQueues(ctx, client, action.Payload, rabbitVHost(runtime.Target))
	case ActionGetQueue:
		return executeGetQueue(ctx, client, action.Payload, rabbitVHost(runtime.Target))
	case ActionListBindings:
		return executeListBindings(ctx, client, action.Payload, rabbitVHost(runtime.Target))
	case ActionPeekMessages:
		return executePeekMessages(ctx, client, action.Payload, rabbitVHost(runtime.Target))
	case ActionPublish:
		return executePublishMessage(ctx, client, action.Payload, rabbitVHost(runtime.Target))
	default:
		return connectors.ActionResult{}, ErrUnsupportedAction
	}
}

func (Connector) TestConnection(ctx context.Context, runtime connectors.RuntimeContext) (connectors.TestResult, error) {
	client, err := newRabbitClient(ctx, runtime)
	if err != nil {
		return connectors.TestResult{Status: classifyRabbitTestError(err), Message: err.Error()}, nil
	}
	var output map[string]any
	if err := client.Get(ctx, "/api/overview", &output); err != nil {
		return connectors.TestResult{Status: classifyRabbitTestError(err), Message: err.Error()}, nil
	}
	return connectors.TestResult{
		Status:  connectors.TestOK,
		Message: "RabbitMQ Management API connection ok.",
		Details: map[string]any{
			"product": output["product_name"],
			"version": output["rabbitmq_version"],
			"vhost":   rabbitVHost(runtime.Target),
		},
	}, nil
}

func executeOverview(ctx context.Context, client *rabbitClient) (connectors.ActionResult, error) {
	var output map[string]any
	if err := client.Get(ctx, "/api/overview", &output); err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: rabbitSummary(output),
	}, nil
}

func executeListVhosts(ctx context.Context, client *rabbitClient) (connectors.ActionResult, error) {
	var rows []map[string]any
	if err := client.Get(ctx, "/api/vhosts", &rows); err != nil {
		return connectors.ActionResult{}, err
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if name := strings.TrimSpace(fmt.Sprint(row["name"])); name != "" {
			names = append(names, name)
		}
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"vhosts": rows, "names": names, "count": len(rows)},
		DisplayText: strings.Join(names, "\n"),
	}, nil
}

func executeListQueues(ctx context.Context, client *rabbitClient, input map[string]any, fallbackVHost string) (connectors.ActionResult, error) {
	vhost := normalizeVHost(input, "vhost", fallbackVHost)
	pattern := strings.ToLower(strings.TrimSpace(stringValue(input, "pattern")))
	limit := normalizeInt(input, "limit", defaultQueueLimit, 1, maxQueueLimit)
	var rows []map[string]any
	if err := client.Get(ctx, "/api/queues/"+pathPart(vhost), &rows); err != nil {
		return connectors.ActionResult{}, err
	}
	filtered := make([]map[string]any, 0, min(len(rows), limit))
	truncated := false
	for _, row := range rows {
		name := strings.TrimSpace(fmt.Sprint(row["name"]))
		if pattern != "" && !strings.Contains(strings.ToLower(name), pattern) {
			continue
		}
		if len(filtered) >= limit {
			truncated = true
			break
		}
		filtered = append(filtered, slimQueue(row))
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"vhost":     vhost,
			"pattern":   pattern,
			"queues":    filtered,
			"count":     len(filtered),
			"truncated": truncated,
		},
		DisplayText: queueListDisplay(filtered),
	}, nil
}

func executeGetQueue(ctx context.Context, client *rabbitClient, input map[string]any, fallbackVHost string) (connectors.ActionResult, error) {
	vhost := normalizeVHost(input, "vhost", fallbackVHost)
	queue := strings.TrimSpace(stringValue(input, "queue"))
	if queue == "" {
		return connectors.ActionResult{}, fmt.Errorf("queue is required")
	}
	var output map[string]any
	if err := client.Get(ctx, "/api/queues/"+pathPart(vhost)+"/"+pathPart(queue), &output); err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: queueDetailDisplay(output),
	}, nil
}

func executeListBindings(ctx context.Context, client *rabbitClient, input map[string]any, fallbackVHost string) (connectors.ActionResult, error) {
	vhost := normalizeVHost(input, "vhost", fallbackVHost)
	queue := strings.TrimSpace(stringValue(input, "queue"))
	limit := normalizeInt(input, "limit", defaultQueueLimit, 1, maxQueueLimit)
	path := "/api/bindings/" + pathPart(vhost)
	if queue != "" {
		path = "/api/queues/" + pathPart(vhost) + "/" + pathPart(queue) + "/bindings"
	}
	var rows []map[string]any
	if err := client.Get(ctx, path, &rows); err != nil {
		return connectors.ActionResult{}, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"vhost":    vhost,
			"queue":    queue,
			"bindings": rows,
			"count":    len(rows),
		},
		DisplayText: fmt.Sprintf("%d binding(s)", len(rows)),
	}, nil
}

func executePeekMessages(ctx context.Context, client *rabbitClient, input map[string]any, fallbackVHost string) (connectors.ActionResult, error) {
	vhost := normalizeVHost(input, "vhost", fallbackVHost)
	queue := strings.TrimSpace(stringValue(input, "queue"))
	if queue == "" {
		return connectors.ActionResult{}, fmt.Errorf("queue is required")
	}
	count := normalizeInt(input, "count", defaultPeekCount, 1, maxPeekCount)
	maxBytes := normalizeInt(input, "max_payload_bytes", defaultPayloadMaxBytes, 1, maxPayloadBytes)
	body := map[string]any{
		"count":    count,
		"ackmode":  "ack_requeue_true",
		"encoding": "auto",
		"truncate": maxBytes,
	}
	var rows []map[string]any
	if err := client.Post(ctx, "/api/queues/"+pathPart(vhost)+"/"+pathPart(queue)+"/get", body, &rows); err != nil {
		return connectors.ActionResult{}, err
	}
	for _, row := range rows {
		if payload, ok := row["payload"].(string); ok && len(payload) > maxBytes {
			row["payload"] = truncateString(payload, maxBytes)
			row["payload_truncated_by_gateway"] = true
		}
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"vhost":              vhost,
			"queue":              queue,
			"ackmode":            "ack_requeue_true",
			"messages":           rows,
			"count":              len(rows),
			"max_payload_bytes":  maxBytes,
			"management_api_get": true,
		},
		DisplayText: fmt.Sprintf("Peeked %d message(s) from %s/%s with requeue.", len(rows), vhost, queue),
	}, nil
}

func executePublishMessage(ctx context.Context, client *rabbitClient, input map[string]any, fallbackVHost string) (connectors.ActionResult, error) {
	vhost := normalizeVHost(input, "vhost", fallbackVHost)
	exchange := strings.TrimSpace(stringValue(input, "exchange"))
	if exchange == "" {
		exchange = "amq.default"
	}
	routingKey := strings.TrimSpace(stringValue(input, "routing_key"))
	if routingKey == "" {
		return connectors.ActionResult{}, fmt.Errorf("routing_key is required")
	}
	payload := stringValue(input, "payload")
	if payload == "" {
		return connectors.ActionResult{}, fmt.Errorf("payload is required")
	}
	if len(payload) > maxPublishPayloadBytes {
		return connectors.ActionResult{}, fmt.Errorf("payload is larger than %d bytes", maxPublishPayloadBytes)
	}
	encoding := strings.ToLower(strings.TrimSpace(stringValue(input, "payload_encoding")))
	if encoding == "" {
		encoding = "string"
	}
	properties, err := normalizeJSONMap(input, "properties")
	if err != nil {
		return connectors.ActionResult{}, err
	}
	body := map[string]any{
		"properties":       properties,
		"routing_key":      routingKey,
		"payload":          payload,
		"payload_encoding": encoding,
	}
	var output map[string]any
	if err := client.Post(ctx, "/api/exchanges/"+pathPart(vhost)+"/"+pathPart(exchange)+"/publish", body, &output); err != nil {
		return connectors.ActionResult{}, err
	}
	routed, _ := output["routed"].(bool)
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"vhost":            vhost,
			"exchange":         exchange,
			"routing_key":      routingKey,
			"payload_bytes":    len(payload),
			"payload_encoding": encoding,
			"routed":           routed,
		},
		DisplayText: fmt.Sprintf("Published %d byte(s) to %s/%s routing_key=%q routed=%t.", len(payload), vhost, exchange, routingKey, routed),
	}, nil
}

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
	if err != nil || strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("%w: password is required", ErrMissingSecret)
	}
	scheme := rabbitScheme(runtime.Target)
	host := rabbitHost(runtime.Target)
	port := rabbitPort(runtime.Target)
	request := connectors.NetworkDialRequest{
		Mode:               connectionMode(runtime.Target),
		Host:               host,
		Port:               port,
		TransportTargetRef: strings.TrimSpace(stringValue(runtime.Target.Config, "transport_target_ref")),
	}
	httpTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			return transport.DialConnectorTCP(ctx, request)
		},
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &rabbitClient{
		baseURL:  fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(host, strconv.Itoa(port))),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout:   rabbitHTTPTimeout,
			Transport: httpTransport,
		},
	}, nil
}

func (client *rabbitClient) Get(ctx context.Context, path string, out any) error {
	return client.Do(ctx, http.MethodGet, path, nil, out)
}

func (client *rabbitClient) Post(ctx context.Context, path string, payload any, out any) error {
	return client.Do(ctx, http.MethodPost, path, payload, out)
}

func (client *rabbitClient) Do(ctx context.Context, method string, path string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(client.username, client.password)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRabbitHTTPBodyBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxRabbitHTTPBodyBytes {
		return fmt.Errorf("rabbitmq response is larger than %d bytes", maxRabbitHTTPBodyBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return rabbitHTTPError(resp.StatusCode, data)
	}
	if out == nil {
		return nil
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode rabbitmq response: %w", err)
	}
	return nil
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
	if scheme != "https" {
		return defaultRabbitMQScheme
	}
	return scheme
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
