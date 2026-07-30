package kafka

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	maxClusterBrokers       = 200
	maxTopicRows            = 1000
	maxTopicPartitions      = 1000
	maxConsumerGroupRows    = 1000
	maxConsumerGroupMembers = 500
	maxMemberAssignments    = 500
	maxConsumerLagRows      = 2000
	maxMetadataOutputBytes  = 512 << 10
	maxMessageOutputBytes   = 500 << 10
)

func executeClusterInfo(ctx context.Context, client *kgo.Client) (connectors.ActionResult, error) {
	metadata, err := clientBrokerMetadata(ctx, client)
	if err != nil {
		return failedResult(err), nil
	}
	brokers := make([]map[string]any, 0, min(len(metadata.Brokers), maxClusterBrokers))
	usedBytes := 0
	truncated := false
	for _, broker := range metadata.Brokers {
		if !appendBoundedRow(&brokers, map[string]any{"id": broker.NodeID, "host": broker.Host, "port": broker.Port}, &usedBytes, maxClusterBrokers, maxMetadataOutputBytes) {
			truncated = true
			break
		}
	}
	output := map[string]any{
		"cluster_id":    metadata.Cluster,
		"controller_id": metadata.Controller,
		"broker_count":  len(metadata.Brokers),
		"brokers_shown": len(brokers),
		"brokers":       brokers,
		"truncated":     truncated,
	}
	return completedResult(output, fmt.Sprintf("Read cluster metadata from %d broker(s).", len(brokers))), nil
}

func executeListTopics(ctx context.Context, client *kgo.Client, includeInternal bool) (connectors.ActionResult, error) {
	requestCtx, admin, cancel := newAdminRequest(ctx, client, 15*time.Second)
	defer cancel()
	topics, err := admin.ListTopicsWithInternal(requestCtx)
	if err != nil {
		return failedResult(err), nil
	}
	if err := topics.Error(); err != nil {
		return failedResult(err), nil
	}
	rows := make([]map[string]any, 0, min(len(topics), maxTopicRows))
	usedBytes := 0
	totalVisible := 0
	truncated := false
	for _, topic := range topics.Sorted() {
		if topic.IsInternal && !includeInternal {
			continue
		}
		totalVisible++
		if !appendBoundedRow(&rows, map[string]any{
			"name":               topic.Topic,
			"internal":           topic.IsInternal,
			"partition_count":    len(topic.Partitions),
			"replication_factor": topic.Partitions.NumReplicas(),
		}, &usedBytes, maxTopicRows, maxMetadataOutputBytes) {
			truncated = true
		}
	}
	return completedResult(map[string]any{"topics": rows, "count": len(rows), "total_visible_count": totalVisible, "include_internal": includeInternal, "truncated": truncated}, fmt.Sprintf("Listed %d topic(s).", len(rows))), nil
}

func executeDescribeTopic(ctx context.Context, client *kgo.Client, topicName string) (connectors.ActionResult, error) {
	requestCtx, admin, cancel := newAdminRequest(ctx, client, 15*time.Second)
	defer cancel()
	metadata, err := admin.ListTopics(requestCtx, topicName)
	if err != nil {
		return failedResult(err), nil
	}
	topic, ok := metadata[topicName]
	if !ok || topic.Err != nil {
		if ok && topic.Err != nil {
			return failedResult(topic.Err), nil
		}
		return failedResult(fmt.Errorf("topic %q was not found or is not visible", topicName)), nil
	}
	offsets, err := admin.ListEndOffsets(requestCtx, topicName)
	if err != nil {
		return failedResult(err), nil
	}
	partitions := make([]map[string]any, 0, min(len(topic.Partitions), maxTopicPartitions))
	usedBytes := 0
	truncated := false
	for _, partition := range topic.Partitions.Sorted() {
		row := map[string]any{
			"partition":        partition.Partition,
			"leader":           partition.Leader,
			"leader_epoch":     partition.LeaderEpoch,
			"replicas":         partition.Replicas,
			"in_sync_replicas": partition.ISR,
			"offline_replicas": partition.OfflineReplicas,
		}
		if offset, found := offsets.Lookup(topicName, partition.Partition); found && offset.Err == nil {
			row["end_offset"] = strconv.FormatInt(offset.Offset, 10)
		}
		if !appendBoundedRow(&partitions, row, &usedBytes, maxTopicPartitions, maxMetadataOutputBytes) {
			truncated = true
		}
	}
	output := map[string]any{
		"name":               topic.Topic,
		"id":                 topic.ID.String(),
		"internal":           topic.IsInternal,
		"partition_count":    len(topic.Partitions),
		"partitions_shown":   len(partitions),
		"replication_factor": topic.Partitions.NumReplicas(),
		"partitions":         partitions,
		"truncated":          truncated,
	}
	return completedResult(output, fmt.Sprintf("Described topic %s with %d partition(s).", topicName, len(partitions))), nil
}

func executeListConsumerGroups(ctx context.Context, client *kgo.Client) (connectors.ActionResult, error) {
	requestCtx, admin, cancel := newAdminRequest(ctx, client, 15*time.Second)
	defer cancel()
	groups, err := admin.ListGroups(requestCtx)
	if err != nil {
		return failedResult(err), nil
	}
	rows := make([]map[string]any, 0, min(len(groups), maxConsumerGroupRows))
	usedBytes := 0
	truncated := false
	for _, group := range groups.Sorted() {
		if !appendBoundedRow(&rows, map[string]any{
			"name":          group.Group,
			"state":         group.State,
			"protocol_type": group.ProtocolType,
			"coordinator":   group.Coordinator,
		}, &usedBytes, maxConsumerGroupRows, maxMetadataOutputBytes) {
			truncated = true
		}
	}
	return completedResult(map[string]any{"consumer_groups": rows, "count": len(rows), "total_visible_count": len(groups), "truncated": truncated}, fmt.Sprintf("Listed %d consumer group(s).", len(rows))), nil
}

func executeDescribeConsumerGroup(ctx context.Context, client *kgo.Client, groupName string) (connectors.ActionResult, error) {
	requestCtx, admin, cancel := newAdminRequest(ctx, client, 20*time.Second)
	defer cancel()
	lags, err := admin.Lag(requestCtx, groupName)
	if err != nil {
		return failedResult(err), nil
	}
	lag, ok := lags[groupName]
	if !ok {
		return failedResult(fmt.Errorf("consumer group %q was not found or is not visible", groupName)), nil
	}
	if err := lag.Error(); err != nil {
		return failedResult(err), nil
	}
	members := make([]map[string]any, 0, min(len(lag.Members), maxConsumerGroupMembers))
	memberBytes := 0
	truncated := false
	for _, member := range lag.Members {
		memberRow := map[string]any{
			"member_id":   member.MemberID,
			"client_id":   member.ClientID,
			"client_host": member.ClientHost,
		}
		if member.InstanceID != nil {
			memberRow["instance_id"] = *member.InstanceID
		}
		if assignment, assignmentOK := member.Assigned.AsConsumer(); assignmentOK {
			topics := make([]map[string]any, 0, len(assignment.Topics))
			for _, assignedTopic := range assignment.Topics {
				if len(topics) >= maxMemberAssignments {
					truncated = true
					break
				}
				topics = append(topics, map[string]any{"topic": assignedTopic.Topic, "partitions": assignedTopic.Partitions})
			}
			memberRow["assignments"] = topics
		}
		if !appendBoundedRow(&members, memberRow, &memberBytes, maxConsumerGroupMembers, maxMetadataOutputBytes/2) {
			truncated = true
		}
	}
	partitionLag := make([]map[string]any, 0, min(len(lag.Lag), maxConsumerLagRows))
	lagBytes := 0
	var totalLag int64
	lagComplete := true
	for _, item := range lag.Lag.Sorted() {
		if item.Err != nil {
			lagComplete = false
			if !appendBoundedRow(&partitionLag, map[string]any{
				"topic": item.Topic, "partition": item.Partition, "error": item.Err.Error(),
			}, &lagBytes, maxConsumerLagRows, maxMetadataOutputBytes/2) {
				truncated = true
			}
			continue
		}
		if item.Lag > 0 {
			totalLag += item.Lag
		}
		row := map[string]any{
			"topic":            item.Topic,
			"partition":        item.Partition,
			"committed_offset": strconv.FormatInt(item.Commit.At, 10),
			"end_offset":       strconv.FormatInt(item.End.Offset, 10),
			"lag":              strconv.FormatInt(item.Lag, 10),
		}
		if item.Start.Err == nil {
			row["earliest_offset"] = strconv.FormatInt(item.Start.Offset, 10)
		}
		if item.Member != nil {
			row["member_id"] = item.Member.MemberID
		}
		if !appendBoundedRow(&partitionLag, row, &lagBytes, maxConsumerLagRows, maxMetadataOutputBytes/2) {
			truncated = true
		}
	}
	output := map[string]any{
		"name":          lag.Group,
		"state":         lag.State,
		"protocol_type": lag.ProtocolType,
		"protocol":      lag.Protocol,
		"coordinator":   lag.Coordinator.NodeID,
		"member_count":  len(lag.Members),
		"members_shown": len(members),
		"total_lag":     strconv.FormatInt(totalLag, 10),
		"lag_complete":  lagComplete,
		"members":       members,
		"partitions":    partitionLag,
		"truncated":     truncated,
	}
	return completedResult(output, fmt.Sprintf("Described consumer group %s with total lag %d.", groupName, totalLag)), nil
}

type readMessagesRequest struct {
	Topic       string
	Partition   int32
	Start       string
	Offset      int64
	MaxRecords  int
	MaxBytes    int
	WaitSeconds int
}

func executeReadMessages(ctx context.Context, runtime connectors.RuntimeContext, req readMessagesRequest) (connectors.ActionResult, error) {
	metadataClient, err := newClient(ctx, runtime)
	if err != nil {
		return failedResult(err), nil
	}
	startOffset, endOffset, err := resolveReadOffsets(ctx, metadataClient, req)
	metadataClient.Close()
	if err != nil {
		return failedResult(err), nil
	}
	if startOffset >= endOffset {
		output := map[string]any{"topic": req.Topic, "partition": req.Partition, "start_offset": strconv.FormatInt(startOffset, 10), "end_offset": strconv.FormatInt(endOffset, 10), "records": []any{}, "count": 0, "truncated": false}
		return completedResult(output, "No messages are currently available in the requested range."), nil
	}
	fetchMaxBytes, err := checkedInt32(int64(req.MaxBytes), "max_bytes")
	if err != nil {
		return failedResult(err), nil
	}
	brokerMaxReadBytes, err := checkedInt32(int64(req.MaxBytes)+65536, "broker_max_read_bytes")
	if err != nil {
		return failedResult(err), nil
	}

	reader, err := newClient(
		ctx,
		runtime,
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{req.Topic: {req.Partition: kgo.NewOffset().At(startOffset)}}),
		kgo.FetchMaxBytes(fetchMaxBytes),
		kgo.FetchMaxPartitionBytes(fetchMaxBytes),
		kgo.MaxConcurrentFetches(1),
		kgo.BrokerMaxReadBytes(brokerMaxReadBytes),
	)
	if err != nil {
		return failedResult(err), nil
	}
	defer reader.Close()
	readCtx, cancel := context.WithTimeout(ctx, time.Duration(req.WaitSeconds)*time.Second)
	defer cancel()
	fetches := reader.PollFetches(readCtx)
	if fetchErrs := fetches.Errors(); len(fetchErrs) > 0 {
		timedOut := ctx.Err() == nil && readCtx.Err() != nil
		for _, fetchErr := range fetchErrs {
			timedOut = timedOut && errors.Is(fetchErr.Err, readCtx.Err())
		}
		if timedOut {
			return completedReadTimeout(req, startOffset, endOffset), nil
		}
		return failedResult(fetchErrs[0].Err), nil
	}
	return buildReadMessagesResult(req, startOffset, endOffset, fetches.Records()), nil
}

func completedReadTimeout(req readMessagesRequest, startOffset, endOffset int64) connectors.ActionResult {
	return completedResult(map[string]any{
		"topic":        req.Topic,
		"partition":    req.Partition,
		"start_offset": strconv.FormatInt(startOffset, 10),
		"end_offset":   strconv.FormatInt(endOffset, 10),
		"records":      []map[string]any{},
		"count":        0,
		"bytes":        0,
		"truncated":    false,
		"timed_out":    true,
	}, fmt.Sprintf("No messages arrived from %s partition %d within %d second(s).", req.Topic, req.Partition, req.WaitSeconds))
}

func buildReadMessagesResult(req readMessagesRequest, startOffset, endOffset int64, records []*kgo.Record) connectors.ActionResult {
	rows := make([]map[string]any, 0, req.MaxRecords)
	totalBytes := 0
	serializedBytes := 0
	truncated := false
	nextOffset := startOffset
	for _, record := range records {
		if record.Offset >= endOffset {
			break
		}
		if len(rows) >= req.MaxRecords {
			truncated = true
			break
		}
		recordBytes := len(record.Key) + len(record.Value)
		for _, header := range record.Headers {
			recordBytes += len(header.Key) + len(header.Value)
		}
		if recordBytes > req.MaxBytes {
			return failedResult(fmt.Errorf("Kafka message at offset %d uses %d payload bytes, exceeding max_bytes %d; increase max_bytes to read this record", record.Offset, recordBytes, req.MaxBytes))
		}
		if totalBytes+recordBytes > req.MaxBytes {
			truncated = true
			break
		}
		totalBytes += recordBytes
		headers := make([]map[string]any, 0, len(record.Headers))
		for _, header := range record.Headers {
			value, encoding := displayBytes(header.Value)
			headers = append(headers, map[string]any{"key": header.Key, "value": value, "encoding": encoding})
		}
		key, keyEncoding := displayBytes(record.Key)
		value, valueEncoding := displayBytes(record.Value)
		row := map[string]any{
			"topic":          record.Topic,
			"partition":      record.Partition,
			"offset":         strconv.FormatInt(record.Offset, 10),
			"timestamp":      record.Timestamp.UTC().Format(time.RFC3339Nano),
			"key":            key,
			"key_encoding":   keyEncoding,
			"value":          value,
			"value_encoding": valueEncoding,
			"headers":        headers,
		}
		encoded, marshalErr := json.Marshal(row)
		if marshalErr != nil {
			return failedResult(fmt.Errorf("serialize Kafka message sample: %w", marshalErr))
		}
		if len(encoded)+1 > maxMessageOutputBytes {
			return failedResult(fmt.Errorf("Kafka message at offset %d exceeds the bounded output limit", record.Offset))
		}
		if serializedBytes+len(encoded)+1 > maxMessageOutputBytes {
			truncated = true
			break
		}
		serializedBytes += len(encoded) + 1
		rows = append(rows, row)
		nextOffset = record.Offset + 1
	}
	if nextOffset < endOffset {
		truncated = true
	}
	output := map[string]any{
		"topic":        req.Topic,
		"partition":    req.Partition,
		"start_offset": strconv.FormatInt(startOffset, 10),
		"end_offset":   strconv.FormatInt(endOffset, 10),
		"records":      rows,
		"count":        len(rows),
		"bytes":        totalBytes,
		"truncated":    truncated,
	}
	if truncated {
		output["continuation_offset"] = strconv.FormatInt(nextOffset, 10)
	}
	return completedResult(output, fmt.Sprintf("Read %d message(s) from %s partition %d without committing offsets.", len(rows), req.Topic, req.Partition))
}

func resolveReadOffsets(ctx context.Context, client *kgo.Client, req readMessagesRequest) (int64, int64, error) {
	requestCtx, admin, cancel := newAdminRequest(ctx, client, 15*time.Second)
	defer cancel()
	earliest, end, err := partitionBounds(requestCtx, admin, req.Topic, req.Partition)
	if err != nil {
		return 0, 0, err
	}
	switch req.Start {
	case "offset":
		if req.Offset < earliest || req.Offset > end {
			return 0, 0, fmt.Errorf("offset must be between the earliest available offset %d and end offset %d", earliest, end)
		}
		return req.Offset, end, nil
	case "earliest":
		return earliest, end, nil
	case "recent":
		start := end - int64(req.MaxRecords)
		if start < earliest {
			start = earliest
		}
		return start, end, nil
	default:
		return 0, 0, fmt.Errorf("unsupported start_position %q", req.Start)
	}
}

func partitionBounds(ctx context.Context, admin *kadm.Client, topic string, partition int32) (int64, int64, error) {
	starts, err := admin.ListStartOffsets(ctx, topic)
	if err != nil {
		return 0, 0, fmt.Errorf("read earliest offset: %w", err)
	}
	ends, err := admin.ListEndOffsets(ctx, topic)
	if err != nil {
		return 0, 0, fmt.Errorf("read end offset: %w", err)
	}
	start, startOK := starts.Lookup(topic, partition)
	end, endOK := ends.Lookup(topic, partition)
	if !startOK || !endOK {
		return 0, 0, fmt.Errorf("topic %q partition %d was not found or is not visible", topic, partition)
	}
	if start.Err != nil {
		return 0, 0, fmt.Errorf("read earliest offset: %w", start.Err)
	}
	if end.Err != nil {
		return 0, 0, fmt.Errorf("read end offset: %w", end.Err)
	}
	return start.Offset, end.Offset, nil
}

func displayBytes(value []byte) (any, string) {
	if value == nil {
		return nil, "null"
	}
	if len(value) == 0 {
		return "", "utf8"
	}
	if utf8.Valid(value) {
		return string(value), "utf8"
	}
	return base64.StdEncoding.EncodeToString(value), "base64"
}

func appendBoundedRow(rows *[]map[string]any, row map[string]any, usedBytes *int, maxRows, maxBytes int) bool {
	if len(*rows) >= maxRows {
		return false
	}
	encoded, err := json.Marshal(row)
	if err != nil || *usedBytes+len(encoded)+1 > maxBytes {
		return false
	}
	*usedBytes += len(encoded) + 1
	*rows = append(*rows, row)
	return true
}
