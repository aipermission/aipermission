package kafka

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	Kind    = "kafka"
	Label   = "Kafka / Redpanda"
	Version = "0.3"

	ActionClusterInfo            = "cluster_info"
	ActionListTopics             = "list_topics"
	ActionDescribeTopic          = "describe_topic"
	ActionListConsumerGroups     = "list_consumer_groups"
	ActionDescribeConsumerGroup  = "describe_consumer_group"
	ActionReadMessages           = "read_messages"
	ActionPublishMessage         = "publish_message"
	ActionSetConsumerGroupOffset = "set_consumer_group_offset"
)

type Connector struct{}

func New() *Connector { return &Connector{} }

func (*Connector) Kind() string    { return Kind }
func (*Connector) Label() string   { return Label }
func (*Connector) Version() string { return Version }

func (*Connector) TargetSchema() connectors.Schema {
	return connectors.Schema{Fields: []connectors.Field{
		{Name: "server_family", Label: "Server family", Type: connectors.FieldSelect, Required: true, Default: "kafka", Options: []connectors.FieldOption{
			{Value: "kafka", Label: "Apache Kafka"},
			{Value: "redpanda", Label: "Redpanda"},
		}},
		{Name: "connection_mode", Label: "Connection mode", Type: connectors.FieldSelect, Required: true, Default: "direct", Options: []connectors.FieldOption{
			{Value: "direct", Label: "Direct from this gateway"},
			{Value: "over_ssh", Label: "Over an SSH connector profile"},
		}},
		{Name: "bootstrap_brokers", Label: "Bootstrap brokers", Type: connectors.FieldMultiline, Required: true, Description: "One host:port per line or a comma-separated list."},
		{Name: "transport_target_ref", Label: "SSH transport profile", Type: connectors.FieldString},
		{Name: "tls_enabled", Label: "Use TLS", Type: connectors.FieldBoolean, Default: false},
		{Name: "allow_insecure_plain_sasl", Label: "Allow PLAIN SASL without TLS", Type: connectors.FieldBoolean, Default: false, Description: "Explicitly permit plaintext PLAIN credentials on a trusted private network."},
		{Name: "tls_server_name", Label: "TLS server name", Type: connectors.FieldString},
		{Name: "tls_ca_pem", Label: "Custom CA certificate", Type: connectors.FieldFileText},
	}}
}

func (*Connector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{{
		Kind:        "sasl",
		Label:       "SASL profile",
		Description: "Optional PLAIN or SCRAM credentials. Select None for brokers without SASL.",
		Schema: connectors.Schema{Fields: []connectors.Field{
			{Name: "mechanism", Label: "SASL mechanism", Type: connectors.FieldSelect, Required: true, Default: "none", Options: []connectors.FieldOption{
				{Value: "none", Label: "None"},
				{Value: "plain", Label: "PLAIN"},
				{Value: "scram_sha_256", Label: "SCRAM-SHA-256"},
				{Value: "scram_sha_512", Label: "SCRAM-SHA-512"},
			}},
			{Name: "username", Label: "Username", Type: connectors.FieldString},
			{Name: "password", Label: "Password", Type: connectors.FieldSecret, Secret: true},
		}},
	}}
}

func (*Connector) GetHelp(_ context.Context, target connectors.TargetView) (connectors.ConnectorHelp, error) {
	family := familyLabel(stringValue(target.Config, "server_family", "kafka"))
	return connectors.ConnectorHelp{
		Title:       family + " connector",
		Summary:     "Inspect cluster metadata, topics, consumer groups, lag, and bounded message samples. Guarded write actions can publish one message or change one inactive consumer-group partition offset.",
		Connector:   Label,
		ConnectorID: Kind,
		Usage: []string{
			"Call list_topics before describe_topic or read_messages.",
			"Use describe_consumer_group to inspect members, committed offsets, and lag.",
			"read_messages uses explicit partition assignment and never commits consumer offsets.",
			"publish_message writes one bounded message with all-in-sync-replica acknowledgements.",
			"set_consumer_group_offset changes one partition only and rejects active consumer groups.",
		},
		Warnings: []string{
			"Message keys, values, and headers can contain secrets or personal data. Read only what the operator approved.",
			"Read results are bounded, but they are persisted in local encrypted history.",
			"Prefer Prompt for publish_message and set_consumer_group_offset. Offset changes can replay or skip messages.",
		},
	}, nil
}

func (*Connector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return actionDefinitions(), nil
}

func (*Connector) PrepareAction(_ context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	action, ok := actionDefinition(req.ActionName)
	if !ok {
		return connectors.PreparedAction{}, fmt.Errorf("unsupported Kafka action %q", req.ActionName)
	}
	input, err := connectors.NormalizeSchemaValues(action.InputSchema, req.Input)
	if err != nil {
		return connectors.PreparedAction{}, err
	}
	if err := canonicalizeActionInput(req.ActionName, input); err != nil {
		return connectors.PreparedAction{}, err
	}
	if err := validateActionInput(req.ActionName, input); err != nil {
		return connectors.PreparedAction{}, err
	}
	summary := action.Label
	switch req.ActionName {
	case ActionDescribeTopic:
		summary = "Describe topic " + stringValue(input, "topic", "")
	case ActionDescribeConsumerGroup:
		summary = "Describe consumer group " + stringValue(input, "group", "")
	case ActionReadMessages:
		summary = fmt.Sprintf("Read up to %d messages from %s partition %d", intValue(input, "max_records", 20), stringValue(input, "topic", ""), intValue(input, "partition", 0))
	case ActionPublishMessage:
		summary = fmt.Sprintf("Publish one message to %s partition %d", stringValue(input, "topic", ""), intValue(input, "partition", 0))
	case ActionSetConsumerGroupOffset:
		summary = fmt.Sprintf("Set %s/%s partition %d offset to %s", stringValue(input, "group", ""), stringValue(input, "topic", ""), intValue(input, "partition", 0), stringValue(input, "offset", ""))
	}
	preview := copyMap(input)
	if req.ActionName == ActionPublishMessage {
		preview = publishPreview(input)
	}
	return connectors.PreparedAction{
		ConnectorKind: Kind,
		TargetRef:     req.Target.Ref,
		ProfileID:     req.Profile.ID,
		ActionName:    req.ActionName,
		Risk:          action.Risk,
		Title:         action.Label,
		Summary:       summary,
		Preview:       preview,
		Payload:       copyMap(input),
		ContextMaterial: map[string]any{
			"target_config":   req.Target.Config,
			"profile_public":  req.Profile.Public,
			"secret_revision": req.Profile.SecretRevision,
		},
	}, nil
}

func (*Connector) ValidateCredentialProfile(kind string, public, secret map[string]any, previous *connectors.CredentialProfileView) error {
	if kind != "sasl" {
		return fmt.Errorf("unsupported Kafka credential kind %q", kind)
	}
	mechanism := stringValue(public, "mechanism", "none")
	if mechanism == "none" {
		return nil
	}
	if strings.TrimSpace(stringValue(public, "username", "")) == "" {
		return fmt.Errorf("username is required for SASL mechanism %s", mechanism)
	}
	if strings.TrimSpace(stringValue(secret, "password", "")) != "" {
		return nil
	}
	if previous != nil && stringValue(previous.Public, "mechanism", "none") != "none" {
		return nil
	}
	return fmt.Errorf("password is required when enabling SASL")
}

func (c *Connector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	if action.ConnectorKind != Kind {
		return connectors.ActionResult{}, fmt.Errorf("prepared action connector kind %q is not %q", action.ConnectorKind, Kind)
	}
	if _, ok := actionDefinition(action.ActionName); !ok {
		return connectors.ActionResult{}, fmt.Errorf("unsupported Kafka action %q", action.ActionName)
	}
	if err := validateActionInput(action.ActionName, action.Payload); err != nil {
		return failedResult(fmt.Errorf("validate prepared Kafka action: %w", err)), nil
	}
	if action.ActionName == ActionReadMessages {
		partition, err := checkedInt32(int64(intValue(action.Payload, "partition", 0)), "partition")
		if err != nil {
			return failedResult(err), nil
		}
		return executeReadMessages(ctx, runtime, readMessagesRequest{
			Topic:       stringValue(action.Payload, "topic", ""),
			Partition:   partition,
			Start:       stringValue(action.Payload, "start_position", "recent"),
			Offset:      int64Value(action.Payload, "offset", 0),
			MaxRecords:  intValue(action.Payload, "max_records", 20),
			MaxBytes:    intValue(action.Payload, "max_bytes", 262144),
			WaitSeconds: intValue(action.Payload, "wait_seconds", 2),
		})
	}
	if action.ActionName == ActionPublishMessage {
		return executePublishMessage(ctx, runtime, action.Payload)
	}
	client, err := newClient(ctx, runtime)
	if err != nil {
		return failedResult(err), nil
	}
	defer client.Close()

	switch action.ActionName {
	case ActionClusterInfo:
		return executeClusterInfo(ctx, client)
	case ActionListTopics:
		return executeListTopics(ctx, client, boolValue(action.Payload, "include_internal", false))
	case ActionDescribeTopic:
		return executeDescribeTopic(ctx, client, stringValue(action.Payload, "topic", ""))
	case ActionListConsumerGroups:
		return executeListConsumerGroups(ctx, client)
	case ActionDescribeConsumerGroup:
		return executeDescribeConsumerGroup(ctx, client, stringValue(action.Payload, "group", ""))
	case ActionSetConsumerGroupOffset:
		return executeSetConsumerGroupOffset(ctx, client, action.Payload)
	}
	return connectors.ActionResult{}, fmt.Errorf("unsupported Kafka action %q", action.ActionName)
}

func (c *Connector) TestConnection(ctx context.Context, runtime connectors.RuntimeContext) (connectors.TestResult, error) {
	client, err := newClient(ctx, runtime)
	if err != nil {
		return testFailure(err), nil
	}
	defer client.Close()
	metadata, err := clientBrokerMetadata(ctx, client)
	if err != nil {
		return testFailure(err), nil
	}
	return connectors.TestResult{
		Status:  connectors.TestOK,
		Message: fmt.Sprintf("%s cluster is reachable through %d broker(s).", familyLabel(stringValue(runtime.Target.Config, "server_family", "kafka")), len(metadata.Brokers)),
		Details: map[string]any{"cluster_id": metadata.Cluster, "controller_id": metadata.Controller, "broker_count": len(metadata.Brokers)},
	}, nil
}

func actionDefinitions() []connectors.ActionDefinition {
	return []connectors.ActionDefinition{
		{Name: ActionClusterInfo, Label: "Cluster info", Description: "Read cluster id, controller, and bounded broker endpoint metadata.", Category: "cluster", Risk: connectors.RiskRead, InputSchema: connectors.Schema{}, OutputHint: connectors.OutputHint{Format: "json", MaxRows: 200, MaxBytes: 262144}},
		{Name: ActionListTopics, Label: "List topics", Description: "List visible topics with partition and replication metadata.", Category: "topics", Risk: connectors.RiskRead, InputSchema: connectors.Schema{Fields: []connectors.Field{
			{Name: "include_internal", Label: "Include internal topics", Type: connectors.FieldBoolean, Default: false},
		}}, OutputHint: connectors.OutputHint{Format: "table", MaxRows: 1000, MaxBytes: 524288}},
		{Name: ActionDescribeTopic, Label: "Describe topic", Description: "Read partition leaders, replicas, ISR, and end offsets for one topic.", Category: "topics", Risk: connectors.RiskRead, InputSchema: connectors.Schema{Fields: []connectors.Field{
			{Name: "topic", Label: "Topic", Type: connectors.FieldString, Required: true},
		}}, OutputHint: connectors.OutputHint{Format: "json", MaxRows: 1000, MaxBytes: 524288}},
		{Name: ActionListConsumerGroups, Label: "List consumer groups", Description: "List visible consumer groups and their current states.", Category: "consumer_groups", Risk: connectors.RiskRead, InputSchema: connectors.Schema{}, OutputHint: connectors.OutputHint{Format: "table", MaxRows: 1000, MaxBytes: 524288}},
		{Name: ActionDescribeConsumerGroup, Label: "Describe consumer group", Description: "Read group members, assignments, committed offsets, and bounded lag details.", Category: "consumer_groups", Risk: connectors.RiskRead, InputSchema: connectors.Schema{Fields: []connectors.Field{
			{Name: "group", Label: "Consumer group", Type: connectors.FieldString, Required: true},
		}}, OutputHint: connectors.OutputHint{Format: "json", MaxRows: 2000, MaxBytes: 1048576}},
		{Name: ActionReadMessages, Label: "Read messages", Description: "Read a bounded sample from one explicit topic partition without joining a group or committing offsets.", Category: "messages", Risk: connectors.RiskRead, InputSchema: connectors.Schema{Fields: []connectors.Field{
			{Name: "topic", Label: "Topic", Type: connectors.FieldString, Required: true},
			{Name: "partition", Label: "Partition", Type: connectors.FieldInteger, Required: true, Default: 0},
			{Name: "start_position", Label: "Start position", Type: connectors.FieldSelect, Required: true, Default: "recent", Options: []connectors.FieldOption{
				{Value: "recent", Label: "Recent messages"},
				{Value: "earliest", Label: "Earliest available"},
				{Value: "offset", Label: "Explicit offset"},
			}},
			{Name: "offset", Label: "Offset", Type: connectors.FieldString, Default: "0"},
			{Name: "max_records", Label: "Maximum records", Type: connectors.FieldInteger, Default: 20},
			{Name: "max_bytes", Label: "Maximum payload bytes", Type: connectors.FieldInteger, Default: 262144},
			{Name: "wait_seconds", Label: "Wait seconds", Type: connectors.FieldInteger, Default: 2},
		}}, OutputHint: connectors.OutputHint{Format: "json", MaxRows: 100, MaxBytes: 524288}},
		{Name: ActionPublishMessage, Label: "Publish message", Description: "Publish one bounded message to one explicit topic partition with all-in-sync-replica acknowledgements.", Category: "messages", Risk: connectors.RiskWrite, InputSchema: connectors.Schema{Fields: []connectors.Field{
			{Name: "topic", Label: "Topic", Type: connectors.FieldString, Required: true},
			{Name: "partition", Label: "Partition", Type: connectors.FieldInteger, Required: true, Default: 0},
			{Name: "key", Label: "Key", Type: connectors.FieldMultiline, Default: ""},
			{Name: "key_encoding", Label: "Key encoding", Type: connectors.FieldSelect, Required: true, Default: "utf8", Options: []connectors.FieldOption{
				{Value: "utf8", Label: "UTF-8"},
				{Value: "base64", Label: "Base64"},
			}},
			{Name: "value", Label: "Value", Type: connectors.FieldMultiline, Default: ""},
			{Name: "value_encoding", Label: "Value encoding", Type: connectors.FieldSelect, Required: true, Default: "utf8", Options: []connectors.FieldOption{
				{Value: "utf8", Label: "UTF-8"},
				{Value: "base64", Label: "Base64"},
			}},
			{Name: "headers", Label: "Headers", Type: connectors.FieldJSON, Default: []any{}, Description: "Optional array of {key, value, encoding}; encoding is utf8 or base64."},
		}}, SensitiveInputFields: []string{"key", "value", "headers"}, OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 65536}},
		{Name: ActionSetConsumerGroupOffset, Label: "Set consumer group offset", Description: "Change one inactive consumer group's committed offset for one explicit topic partition.", Category: "consumer_groups", Risk: connectors.RiskDestructive, InputSchema: connectors.Schema{Fields: []connectors.Field{
			{Name: "group", Label: "Consumer group", Type: connectors.FieldString, Required: true},
			{Name: "topic", Label: "Topic", Type: connectors.FieldString, Required: true},
			{Name: "partition", Label: "Partition", Type: connectors.FieldInteger, Required: true, Default: 0},
			{Name: "offset", Label: "New offset", Type: connectors.FieldString, Required: true},
		}}, OutputHint: connectors.OutputHint{Format: "json", MaxBytes: 65536}},
	}
}

func actionDefinition(name string) (connectors.ActionDefinition, bool) {
	for _, action := range actionDefinitions() {
		if action.Name == name {
			return action, true
		}
	}
	return connectors.ActionDefinition{}, false
}

func validateActionInput(action string, input map[string]any) error {
	switch action {
	case ActionDescribeTopic, ActionReadMessages, ActionPublishMessage:
		if strings.TrimSpace(stringValue(input, "topic", "")) == "" {
			return fmt.Errorf("topic is required")
		}
	case ActionDescribeConsumerGroup:
		if strings.TrimSpace(stringValue(input, "group", "")) == "" {
			return fmt.Errorf("group is required")
		}
	case ActionSetConsumerGroupOffset:
		if strings.TrimSpace(stringValue(input, "group", "")) == "" {
			return fmt.Errorf("group is required")
		}
		if strings.TrimSpace(stringValue(input, "topic", "")) == "" {
			return fmt.Errorf("topic is required")
		}
	}
	if action == ActionReadMessages || action == ActionPublishMessage || action == ActionSetConsumerGroupOffset {
		if partition := intValue(input, "partition", 0); partition < 0 || partition > 1000000 {
			return fmt.Errorf("partition must be between 0 and 1000000")
		}
	}
	if action == ActionReadMessages {
		if records := intValue(input, "max_records", 20); records < 1 || records > 100 {
			return fmt.Errorf("max_records must be between 1 and 100")
		}
		if bytes := intValue(input, "max_bytes", 262144); bytes < 1 || bytes > 1048576 {
			return fmt.Errorf("max_bytes must be between 1 and 1048576")
		}
		if wait := intValue(input, "wait_seconds", 2); wait < 1 || wait > 10 {
			return fmt.Errorf("wait_seconds must be between 1 and 10")
		}
		if stringValue(input, "start_position", "recent") == "offset" && int64Value(input, "offset", 0) < 0 {
			return fmt.Errorf("offset must be zero or greater")
		}
	}
	if action == ActionPublishMessage {
		if err := validatePublishInput(input); err != nil {
			return err
		}
	}
	if action == ActionSetConsumerGroupOffset && int64Value(input, "offset", -1) < 0 {
		return fmt.Errorf("offset must be zero or greater")
	}
	return nil
}

func canonicalizeActionInput(action string, input map[string]any) error {
	for _, field := range []string{"topic", "group"} {
		if value, ok := input[field]; ok {
			input[field] = strings.TrimSpace(fmt.Sprint(value))
		}
	}
	integerFields := []string{}
	switch action {
	case ActionReadMessages:
		integerFields = []string{"partition", "max_records", "max_bytes", "wait_seconds"}
	case ActionPublishMessage, ActionSetConsumerGroupOffset:
		integerFields = []string{"partition"}
	}
	for _, field := range integerFields {
		value, err := exactInt(input[field], field)
		if err != nil {
			return err
		}
		input[field] = value
	}
	if action == ActionReadMessages && stringValue(input, "start_position", "recent") != "offset" {
		input["offset"] = "0"
	} else if action == ActionReadMessages || action == ActionSetConsumerGroupOffset {
		offset, err := exactInteger(input["offset"], "offset")
		if err != nil {
			return err
		}
		input["offset"] = strconv.FormatInt(offset, 10)
	}
	if action == ActionPublishMessage {
		headers, err := canonicalPublishHeaders(input["headers"])
		if err != nil {
			return err
		}
		input["headers"] = headers
	}
	return nil
}

func exactInteger(value any, field string) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed || math.Abs(typed) > 9007199254740991 {
			return 0, fmt.Errorf("%s must be an exact integer", field)
		}
		return int64(typed), nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s must be an exact base-10 integer", field)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("%s must be an exact integer", field)
	}
}

func exactInt(value any, field string) (int, error) {
	exact, err := exactInteger(value, field)
	if err != nil {
		return 0, err
	}
	converted, err := strconv.Atoi(strconv.FormatInt(exact, 10))
	if err != nil {
		return 0, fmt.Errorf("%s exceeds the supported integer range", field)
	}
	return converted, nil
}

func checkedInt32(value int64, field string) (int32, error) {
	const (
		minInt32 = -1 << 31
		maxInt32 = 1<<31 - 1
	)
	if value < minInt32 || value > maxInt32 {
		return 0, fmt.Errorf("%s exceeds the supported 32-bit integer range", field)
	}
	return int32(value), nil
}

func exactInt32(value any, field string) (int32, error) {
	if text, ok := value.(string); ok {
		parsed, err := strconv.ParseInt(strings.TrimSpace(text), 10, 32)
		if err != nil {
			return 0, fmt.Errorf("%s must be an exact 32-bit base-10 integer", field)
		}
		return int32(parsed), nil
	}
	exact, err := exactInteger(value, field)
	if err != nil {
		return 0, err
	}
	return checkedInt32(exact, field)
}

func failedResult(err error) connectors.ActionResult {
	return connectors.ActionResult{Status: connectors.ResultFailed, Error: err.Error(), DisplayText: err.Error()}
}

func outcomeUnknownResult(stage string, err error) connectors.ActionResult {
	message := "Kafka mutation outcome is unknown after dispatch; inspect broker state before retrying"
	if err != nil {
		message += ": " + err.Error()
	}
	return connectors.ActionResult{
		Status:      connectors.ResultOutcomeUnknown,
		Output:      map[string]any{"dispatch_stage": stage, "retry_safe": false},
		Error:       message,
		DisplayText: message,
	}
}

func completedResult(output any, display string) connectors.ActionResult {
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: output, DisplayText: display}
}

func copyMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func familyLabel(value string) string {
	if value == "redpanda" {
		return "Redpanda"
	}
	return "Kafka"
}
