package kafka

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	maxPublishKeyBytes         = 64 << 10
	maxPublishValueBytes       = 1 << 20
	maxPublishHeaderCount      = 64
	maxPublishHeaderKeyBytes   = 256
	maxPublishHeaderValueBytes = 64 << 10
	maxPublishRecordBytes      = 1 << 20
	maxProducerBatchBytes      = maxPublishRecordBytes + (64 << 10)
)

type publishHeader struct {
	Key      string  `json:"key"`
	Value    *string `json:"value"`
	Encoding string  `json:"encoding,omitempty"`
}

func validatePublishInput(input map[string]any) error {
	if len(stringValue(input, "topic", "")) > 249 {
		return fmt.Errorf("topic must not exceed 249 bytes")
	}
	key, err := decodePublishBytes(stringValue(input, "key", ""), stringValue(input, "key_encoding", "utf8"), "key", maxPublishKeyBytes)
	if err != nil {
		return err
	}
	value, err := decodePublishBytes(stringValue(input, "value", ""), stringValue(input, "value_encoding", "utf8"), "value", maxPublishValueBytes)
	if err != nil {
		return err
	}
	headers, err := parsePublishHeaders(input["headers"])
	if err != nil {
		return err
	}
	total := len(key) + len(value)
	for _, header := range headers {
		total += len(header.Key) + len(header.Value)
	}
	if total > maxPublishRecordBytes {
		return fmt.Errorf("combined key, value, and headers must not exceed %d bytes", maxPublishRecordBytes)
	}
	return nil
}

func publishPreview(input map[string]any) map[string]any {
	key, _ := decodePublishBytes(stringValue(input, "key", ""), stringValue(input, "key_encoding", "utf8"), "key", maxPublishKeyBytes)
	value, _ := decodePublishBytes(stringValue(input, "value", ""), stringValue(input, "value_encoding", "utf8"), "value", maxPublishValueBytes)
	headers, _ := parsePublishHeaders(input["headers"])
	return map[string]any{
		"topic":         stringValue(input, "topic", ""),
		"partition":     intValue(input, "partition", 0),
		"key_bytes":     len(key),
		"value_bytes":   len(value),
		"headers_count": len(headers),
	}
}

func executePublishMessage(ctx context.Context, runtime connectors.RuntimeContext, payload map[string]any) (connectors.ActionResult, error) {
	topic := stringValue(payload, "topic", "")
	partition, err := exactInt32(payload["partition"], "partition")
	if err != nil {
		return failedResult(err), nil
	}
	key, err := decodePublishBytes(stringValue(payload, "key", ""), stringValue(payload, "key_encoding", "utf8"), "key", maxPublishKeyBytes)
	if err != nil {
		return failedResult(err), nil
	}
	value, err := decodePublishBytes(stringValue(payload, "value", ""), stringValue(payload, "value_encoding", "utf8"), "value", maxPublishValueBytes)
	if err != nil {
		return failedResult(err), nil
	}
	headers, err := parsePublishHeaders(payload["headers"])
	if err != nil {
		return failedResult(err), nil
	}
	client, err := newClient(
		ctx,
		runtime,
		kgo.RecordPartitioner(kgo.ManualPartitioner()),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.DisableIdempotentWrite(),
		kgo.RecordRetries(0),
		kgo.UnknownTopicRetries(0),
		kgo.RecordDeliveryTimeout(15*time.Second),
		kgo.ProduceRequestTimeout(10*time.Second),
		kgo.ProducerBatchMaxBytes(maxProducerBatchBytes),
	)
	if err != nil {
		return failedResult(err), nil
	}
	defer client.Close()
	record := &kgo.Record{
		Topic:     topic,
		Partition: partition,
		Key:       key,
		Value:     value,
		Headers:   headers,
	}
	publishCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result := client.ProduceSync(publishCtx, record)
	if err := result.FirstErr(); err != nil {
		return classifyKafkaPublishFailure(err), nil
	}
	output := map[string]any{
		"topic":         record.Topic,
		"partition":     record.Partition,
		"offset":        strconv.FormatInt(record.Offset, 10),
		"timestamp":     record.Timestamp.UTC().Format(time.RFC3339Nano),
		"key_bytes":     len(key),
		"value_bytes":   len(value),
		"headers_count": len(headers),
	}
	return completedResult(output, fmt.Sprintf("Published one message to %s partition %d at offset %d.", record.Topic, record.Partition, record.Offset)), nil
}

func classifyKafkaPublishFailure(err error) connectors.ActionResult {
	if isDefiniteKafkaPublishRejection(err) {
		return failedResult(fmt.Errorf("publish Kafka message rejected by broker: %w", err))
	}
	return outcomeUnknownResult("produce_request", fmt.Errorf("publish Kafka message: %w; delivery may be unknown, inspect the topic before retrying", err))
}

func isDefiniteKafkaPublishRejection(err error) bool {
	for _, definite := range []error{
		kerr.TopicAuthorizationFailed,
		kerr.ClusterAuthorizationFailed,
		kerr.InvalidTopicException,
		kerr.MessageTooLarge,
		kerr.RecordListTooLarge,
		kerr.InvalidRequiredAcks,
		kerr.UnsupportedForMessageFormat,
		kerr.PolicyViolation,
	} {
		if errors.Is(err, definite) {
			return true
		}
	}
	return false
}

func executeSetConsumerGroupOffset(ctx context.Context, client *kgo.Client, payload map[string]any) (connectors.ActionResult, error) {
	group := stringValue(payload, "group", "")
	topic := stringValue(payload, "topic", "")
	partition, err := exactInt32(payload["partition"], "partition")
	if err != nil {
		return failedResult(err), nil
	}
	offset := int64Value(payload, "offset", -1)
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	admin := kadm.NewClient(client)

	described, err := requireInactiveConsumerGroup(requestCtx, admin, group)
	if err != nil {
		return failedResult(err), nil
	}

	earliest, end, err := partitionBounds(requestCtx, admin, topic, partition)
	if err != nil {
		return failedResult(err), nil
	}
	if offset < earliest || offset > end {
		return failedResult(fmt.Errorf("offset must be between earliest offset %d and end offset %d", earliest, end)), nil
	}

	previous := int64(-1)
	committed, err := admin.FetchOffsets(kadm.RequireStable(requestCtx), group)
	if err != nil {
		return failedResult(fmt.Errorf("read current consumer group offset: %w", err)), nil
	}
	if current, found := committed.Lookup(topic, partition); found {
		if current.Err != nil {
			return failedResult(fmt.Errorf("read current consumer group offset: %w", current.Err)), nil
		}
		previous = current.At
	}
	offsets := kadm.Offsets{}
	offsets.Add(kadm.Offset{Topic: topic, Partition: partition, At: offset, LeaderEpoch: -1})
	described, err = requireInactiveConsumerGroup(requestCtx, admin, group)
	if err != nil {
		return failedResult(err), nil
	}
	responses, err := admin.CommitOffsets(requestCtx, group, offsets)
	if err != nil {
		return outcomeUnknownResult("offset_commit_request", fmt.Errorf("commit consumer group offset: %w", err)), nil
	}
	if err := responses.Error(); err != nil {
		return failedResult(fmt.Errorf("commit consumer group offset: %w", err)), nil
	}
	verified, err := admin.FetchOffsets(kadm.RequireStable(requestCtx), group)
	if err != nil {
		return outcomeUnknownResult("offset_commit_verification", fmt.Errorf("verify consumer group offset: %w", err)), nil
	}
	actual, found := verified.Lookup(topic, partition)
	if !found || actual.Err != nil || actual.At != offset {
		return outcomeUnknownResult("offset_commit_verification", errors.New("consumer group offset verification failed")), nil
	}
	postCommitState := "unknown"
	postCommitWarning := ""
	postCommitCtx, postCommitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer postCommitCancel()
	postCommitGroup, postCommitErr := requireInactiveConsumerGroup(postCommitCtx, admin, group)
	if postCommitErr != nil {
		postCommitWarning = "The offset was committed and verified, but the consumer group was no longer confirmed inactive afterward: " + postCommitErr.Error()
	} else {
		postCommitState = postCommitGroup.State
	}
	output := map[string]any{
		"group":                  group,
		"topic":                  topic,
		"partition":              partition,
		"previous_offset":        strconv.FormatInt(previous, 10),
		"new_offset":             strconv.FormatInt(offset, 10),
		"earliest_offset":        strconv.FormatInt(earliest, 10),
		"end_offset":             strconv.FormatInt(end, 10),
		"group_state_before":     described.State,
		"group_state_after":      postCommitState,
		"inactive_guard":         "best_effort",
		"post_commit_warning":    postCommitWarning,
		"offset_commit_verified": true,
	}
	return completedResult(output, fmt.Sprintf("Set consumer group %s offset for %s partition %d from %d to %d.", group, topic, partition, previous, offset)), nil
}

func requireInactiveConsumerGroup(ctx context.Context, admin *kadm.Client, group string) (kadm.DescribedGroup, error) {
	modernGroups, modernErr := admin.DescribeConsumerGroups(ctx, group)
	if modernErr == nil {
		if modern, ok := modernGroups[group]; ok {
			if modern.Err == nil {
				return kadm.DescribedGroup{}, fmt.Errorf("consumer group %q uses the modern consumer protocol; changing offsets for modern groups is not supported", group)
			}
			if !errors.Is(modern.Err, kerr.GroupIDNotFound) && !errors.Is(modern.Err, kerr.UnsupportedVersion) {
				return kadm.DescribedGroup{}, fmt.Errorf("inspect modern consumer group before offset change: %w", modern.Err)
			}
		}
	} else if !errors.Is(modernErr, kerr.GroupIDNotFound) && !errors.Is(modernErr, kerr.UnsupportedVersion) {
		return kadm.DescribedGroup{}, fmt.Errorf("inspect modern consumer group before offset change: %w", modernErr)
	}

	groups, err := admin.DescribeGroups(ctx, group)
	if err != nil {
		return kadm.DescribedGroup{}, fmt.Errorf("describe consumer group before offset change: %w", err)
	}
	described, ok := groups[group]
	if !ok || described.Err != nil {
		if ok && described.Err != nil {
			return kadm.DescribedGroup{}, fmt.Errorf("describe consumer group before offset change: %w", described.Err)
		}
		return kadm.DescribedGroup{}, fmt.Errorf("consumer group %q was not found or is not visible", group)
	}
	if len(described.Members) != 0 || (described.State != "Empty" && described.State != "Dead") {
		return kadm.DescribedGroup{}, fmt.Errorf("consumer group %q must be inactive before changing offsets; current state is %s with %d member(s)", group, described.State, len(described.Members))
	}
	return described, nil
}

func parsePublishHeaders(value any) ([]kgo.RecordHeader, error) {
	entries, err := canonicalPublishHeaders(value)
	if err != nil {
		return nil, err
	}
	headers := make([]kgo.RecordHeader, 0, len(entries))
	for index, entry := range entries {
		decoded, err := decodePublishBytes(entry["value"].(string), entry["encoding"].(string), fmt.Sprintf("headers[%d].value", index), maxPublishHeaderValueBytes)
		if err != nil {
			return nil, err
		}
		headers = append(headers, kgo.RecordHeader{Key: entry["key"].(string), Value: decoded})
	}
	return headers, nil
}

func canonicalPublishHeaders(value any) ([]map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	if text, ok := value.(string); ok {
		if strings.TrimSpace(text) == "" {
			return nil, nil
		}
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			return nil, fmt.Errorf("headers must be valid JSON: %w", err)
		}
		value = decoded
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("headers must be a JSON array: %w", err)
	}
	var rawEntries []json.RawMessage
	if err := json.Unmarshal(encoded, &rawEntries); err != nil {
		return nil, fmt.Errorf("headers must be an array of {key, value, encoding}: %w", err)
	}
	if len(rawEntries) > maxPublishHeaderCount {
		return nil, fmt.Errorf("headers count must not exceed %d", maxPublishHeaderCount)
	}
	entries := make([]map[string]any, 0, len(rawEntries))
	for index, raw := range rawEntries {
		var entry publishHeader
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&entry); err != nil {
			return nil, fmt.Errorf("headers[%d] must contain only key, value, and encoding: %w", index, err)
		}
		entry.Key = strings.TrimSpace(entry.Key)
		if entry.Key == "" {
			return nil, fmt.Errorf("headers[%d].key is required", index)
		}
		if len(entry.Key) > maxPublishHeaderKeyBytes {
			return nil, fmt.Errorf("headers[%d].key must not exceed %d bytes", index, maxPublishHeaderKeyBytes)
		}
		if entry.Value == nil {
			return nil, fmt.Errorf("headers[%d].value is required", index)
		}
		encoding := defaultEncoding(entry.Encoding)
		if _, err := decodePublishBytes(*entry.Value, encoding, fmt.Sprintf("headers[%d].value", index), maxPublishHeaderValueBytes); err != nil {
			return nil, err
		}
		entries = append(entries, map[string]any{"key": entry.Key, "value": *entry.Value, "encoding": encoding})
	}
	return entries, nil
}

func decodePublishBytes(value, encoding, field string, limit int) ([]byte, error) {
	var decoded []byte
	switch encoding {
	case "utf8":
		if !utf8.ValidString(value) {
			return nil, fmt.Errorf("%s must contain valid UTF-8", field)
		}
		decoded = []byte(value)
	case "base64":
		var err error
		decoded, err = base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("%s must contain valid base64: %w", field, err)
		}
	default:
		return nil, fmt.Errorf("%s encoding must be utf8 or base64", field)
	}
	if len(decoded) > limit {
		return nil, fmt.Errorf("%s must not exceed %d bytes", field, limit)
	}
	return decoded, nil
}

func defaultEncoding(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "utf8"
	}
	return value
}
