// Package rabbitmqconnector defines the RabbitMQ connector contract.
package rabbitmqconnector

import (
	"context"
	"errors"
	"fmt"
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
		return connectors.ActionResult{}, classifyRabbitMutationError("peek messages", err)
	}
	if rows == nil {
		return connectors.ActionResult{}, connectors.ClassifyOutcomeUnknown(
			"response_validation",
			nil,
			fmt.Errorf("peek messages returned an invalid null response after dispatch"),
		)
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
		return connectors.ActionResult{}, classifyRabbitMutationError("publish message", err)
	}
	routed, ok := output["routed"].(bool)
	if !ok {
		return connectors.ActionResult{}, connectors.ClassifyOutcomeUnknown(
			"management_api_response",
			nil,
			errors.New("RabbitMQ publish outcome is unknown because the management API response omitted the required routed boolean"),
		)
	}
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
