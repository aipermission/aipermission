package rabbitmqconnector

import (
	"context"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

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
			Default:     "auto",
			Description: "Auto uses verified HTTPS for remote direct endpoints and HTTP for local or SSH-tunneled endpoints.",
			Options: []connectors.FieldOption{
				{Value: "auto", Label: "Auto"},
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
			Type:        connectors.FieldInteger,
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
				{Name: "limit", Label: "Limit", Type: connectors.FieldInteger, Default: defaultQueueLimit},
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
				{Name: "limit", Label: "Limit", Type: connectors.FieldInteger, Default: defaultQueueLimit},
			}},
			OutputHint: connectors.OutputHint{Format: "json", MaxRows: maxQueueLimit},
		},
		{
			Name:        ActionPeekMessages,
			Label:       "Peek messages",
			Description: "Inspect and requeue a bounded message preview. RabbitMQ delivery state or order may change.",
			Category:    "browser",
			Risk:        connectors.RiskWrite,
			InputSchema: connectors.Schema{Fields: []connectors.Field{
				{Name: "vhost", Label: "Vhost", Type: connectors.FieldString, Description: "Optional vhost; defaults to target vhost."},
				{Name: "queue", Label: "Queue", Type: connectors.FieldString, Required: true},
				{Name: "count", Label: "Count", Type: connectors.FieldInteger, Default: defaultPeekCount},
				{Name: "max_payload_bytes", Label: "Max payload bytes", Type: connectors.FieldInteger, Default: defaultPayloadMaxBytes},
			}},
			RetryPolicy: connectors.RetryPolicy{Class: connectors.RetryNonIdempotent},
			OutputHint:  connectors.OutputHint{Format: "json", MaxBytes: maxPayloadBytes},
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
			SensitiveInputFields: []string{"payload", "properties"},
			OutputHint:           connectors.OutputHint{Format: "json", MaxBytes: 4000},
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
		risk = connectors.RiskWrite
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
		Dependencies:  connectors.NetworkTransportDependencies(req.Target),
		Risk:          risk,
		Title:         title,
		Summary:       summary,
		Preview:       input,
		Payload:       input,
		ContextMaterial: map[string]any{
			"target":          req.Target.Name,
			"profile":         req.Profile.Label,
			"connection_mode": connectionMode(req.Target),
			"scheme":          rabbitScheme(req.Target),
			"vhost":           vhost,
		},
	}, nil
}
