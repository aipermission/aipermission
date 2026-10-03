package conformance_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestKafkaRealService(t *testing.T) {
	requireConformance(t)
	assertKafkaWireProbe(t)
	host := fixtureHost("AIPERMISSION_KAFKA_HOST", "127.0.0.1")
	if host != "127.0.0.1" && host != "kafka" {
		t.Fatal("Kafka fixture host must be loopback or the owned service alias")
	}
	port := fixturePort(t, "AIPERMISSION_KAFKA_PORT", 0)
	if port == 0 {
		t.Fatal("owned Kafka fixture port is required")
	}
	transport := &kafkaFixtureTransport{host: host, port: port}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	client, err := kgo.NewClient(kgo.SeedBrokers(address), kgo.Dialer(transport.dial), kgo.DialTimeout(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	admin := kadm.NewClient(client)
	metadata, err := admin.Metadata(ctx)
	if err != nil || metadata.Cluster != "q1Cx6uyAT_C32_D-m73BFQ" {
		t.Fatalf("owned broker identity: %v, %v", metadata.Cluster, err)
	}
	topic := fmt.Sprintf("aip67-conformance-%d", time.Now().UnixNano())
	created, err := admin.CreateTopics(ctx, 2, 1, nil, topic)
	if err != nil || created.Error() != nil {
		t.Fatalf("create fixture topic: %v / %v", err, created.Error())
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		deleted, err := admin.DeleteTopics(cleanup, topic)
		if err != nil || deleted.Error() != nil {
			t.Errorf("delete owned fixture topic: %v / %v", err, deleted.Error())
		}
	})
	runtime := connectors.RuntimeContext{
		Target:       connectors.TargetView{Ref: "kafka:1:1", Config: map[string]any{"server_family": "kafka", "connection_mode": "direct", "bootstrap_brokers": address}},
		Profile:      connectors.CredentialProfileView{ID: 1, Public: map[string]any{"mechanism": "none"}},
		Capabilities: transport, Secrets: fixtureSecrets{},
	}
	connector := kafka.New()
	assertConnection(t, connector, runtime)
	assertResultContains(t, executeAction(t, connector, runtime, kafka.ActionClusterInfo, nil), metadata.Cluster)
	assertResultContains(t, executeAction(t, connector, runtime, kafka.ActionDescribeTopic, map[string]any{"topic": topic}), topic)
	key, value, header := []byte{0, 0xff, 17}, bytes.Repeat([]byte{0xfe, 0, 1, 2}, 175), []byte{0x80, 0}
	input := map[string]any{
		"topic": topic, "partition": 1, "key": base64.StdEncoding.EncodeToString(key), "key_encoding": "base64",
		"value": base64.StdEncoding.EncodeToString(value), "value_encoding": "base64",
		"headers": []any{map[string]any{"key": "fixture-header", "value": base64.StdEncoding.EncodeToString(header), "encoding": "base64"}},
	}
	for _, expected := range []string{"0", "1"} {
		output := executeAction(t, connector, runtime, kafka.ActionPublishMessage, input).Output.(map[string]any)
		if output["offset"] != expected || output["partition"] != int32(1) {
			t.Fatalf("publish identity: %#v", output)
		}
	}
	reader, err := kgo.NewClient(kgo.SeedBrokers(address), kgo.Dialer(transport.dial), kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{topic: {1: kgo.NewOffset().AtStart()}}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	fetched := reader.PollFetches(ctx)
	if errs := fetched.Errors(); len(errs) != 0 {
		t.Fatal(errs)
	}
	records := fetched.Records()
	if len(records) != 2 {
		t.Fatalf("broker record count: %d", len(records))
	}
	for index, record := range records {
		if record.Partition != 1 || record.Offset != int64(index) || !bytes.Equal(record.Key, key) || !bytes.Equal(record.Value, value) || len(record.Headers) != 1 || record.Headers[0].Key != "fixture-header" || !bytes.Equal(record.Headers[0].Value, header) {
			t.Fatalf("broker readback identity at %d", index)
		}
	}
	group := topic + "-inactive"
	committed := kadm.Offsets{}
	committed.Add(kadm.Offset{Topic: topic, Partition: 0, At: 0})
	committed.Add(kadm.Offset{Topic: topic, Partition: 1, At: 0})
	response, err := admin.CommitOffsets(ctx, group, committed)
	if err != nil || response.Error() != nil {
		t.Fatalf("seed committed offsets: %v / %v", err, response.Error())
	}
	dialsBefore, commitsBefore, joinsBefore, _ := transport.observed()
	for _, limits := range []struct{ records, bytes int }{{1, 2048}, {10, 1024}} {
		sample := executeAction(t, connector, runtime, kafka.ActionReadMessages, map[string]any{"topic": topic, "partition": 1, "start_position": "earliest", "max_records": limits.records, "max_bytes": limits.bytes, "wait_seconds": 2})
		output := sample.Output.(map[string]any)
		rows := output["records"].([]map[string]any)
		if output["count"] != 1 || len(rows) != 1 || rows[0]["offset"] != "0" || output["truncated"] != true || output["continuation_offset"] != "1" || output["bytes"] != len(key)+len(value)+len("fixture-header")+len(header) {
			t.Fatalf("bounded sample and continuation: %#v", output)
		}
		if rows[0]["key"] != base64.StdEncoding.EncodeToString(key) || rows[0]["key_encoding"] != "base64" || rows[0]["value"] != base64.StdEncoding.EncodeToString(value) || rows[0]["value_encoding"] != "base64" {
			t.Fatal("sample lost binary identity or encodings")
		}
		headers := rows[0]["headers"].([]map[string]any)
		if len(headers) != 1 || headers[0]["key"] != "fixture-header" || headers[0]["value"] != base64.StdEncoding.EncodeToString(header) || headers[0]["encoding"] != "base64" {
			t.Fatal("sample lost binary header identity or encoding")
		}
	}
	dialsAfter, commitsAfter, joinsAfter, invalid := transport.observed()
	if dialsAfter <= dialsBefore || commitsAfter != commitsBefore || joinsAfter != joinsBefore || invalid {
		t.Fatal("sampling bypassed transport observation or dispatched group/commit traffic")
	}
	after, err := admin.FetchOffsets(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	for _, partition := range []int32{0, 1} {
		offset, ok := after.Lookup(topic, partition)
		if !ok || offset.At != 0 || offset.Err != nil {
			t.Fatalf("sample changed group partition %d: %#v", partition, offset)
		}
	}
	executeAction(t, connector, runtime, kafka.ActionSetConsumerGroupOffset, map[string]any{"group": group, "topic": topic, "partition": 1, "offset": "1"})
	_, actualCommits, _, invalid := transport.observed()
	if actualCommits <= commitsAfter || invalid {
		t.Fatal("wire probe did not observe the explicit offset mutation")
	}
	after, err = admin.FetchOffsets(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	for partition, expected := range map[int32]int64{0: 0, 1: 1} {
		offset, ok := after.Lookup(topic, partition)
		if !ok || offset.At != expected || offset.Err != nil {
			t.Fatalf("offset mutation escaped partition %d: %#v", partition, offset)
		}
	}
	assertResultContains(t, executeAction(t, connector, runtime, kafka.ActionDescribeConsumerGroup, map[string]any{"group": group}), group)
	assertKafkaActiveGroupRejected(t, ctx, connector, runtime, transport, address, group, topic, admin)
	dials, _, _, invalid := transport.observed()
	if dials == 0 || invalid {
		t.Fatal("production connector did not use the fixture transport")
	}
	if conn, err := transport.dial(ctx, "tcp", "outside.fixture.invalid:9092"); err == nil || conn != nil {
		t.Fatal("fixture allowed an outside broker dial")
	}
}

func TestKafkaWireProbe(t *testing.T) { assertKafkaWireProbe(t) }

func assertKafkaWireProbe(t *testing.T) {
	t.Helper()
	transport := &kafkaFixtureTransport{}
	probe := &kafkaProbeConn{transport: transport}
	// Two fragmented frames include body bytes that must not become API keys.
	for _, octet := range []byte{0, 0, 0, 4, 0, 8, 0, 11, 0, 0, 0, 2, 0, 11} {
		probe.observeWritten([]byte{octet})
	}
	_, commits, joins, invalid := transport.observed()
	if commits != 1 || joins != 1 || invalid {
		t.Fatal("fragmented request observation failed")
	}
	probe.observeWritten([]byte{0, 0, 0, 1, 0, 8})
	_, _, _, invalid = transport.observed()
	if !invalid {
		t.Fatal("malformed frame did not invalidate the receipt")
	}
}

func assertKafkaActiveGroupRejected(t *testing.T, ctx context.Context, connector *kafka.Connector, runtime connectors.RuntimeContext, transport *kafkaFixtureTransport, address, group, topic string, admin *kadm.Client) {
	t.Helper()
	assigned := make(chan struct{}, 1)
	member, err := kgo.NewClient(kgo.SeedBrokers(address), kgo.Dialer(transport.dial), kgo.ConsumerGroup(group), kgo.ConsumeTopics(topic), kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(func(context.Context, *kgo.Client, map[string][]int32) {
			select {
			case assigned <- struct{}{}:
			default:
			}
		}))
	if err != nil {
		t.Fatal(err)
	}
	pollContext, stop := context.WithCancel(ctx)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		for pollContext.Err() == nil {
			member.PollFetches(pollContext)
		}
	}()
	defer func() {
		stop()
		member.Close()
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("fixture member did not join shutdown")
		}
	}()
	select {
	case <-assigned:
	case <-ctx.Done():
		t.Fatal("real consumer did not join the group")
	}
	prepared, err := connector.PrepareAction(ctx, connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: kafka.ActionSetConsumerGroupOffset,
		Input: map[string]any{"group": group, "topic": topic, "partition": 1, "offset": "0"}, Reason: "owned active-group conformance"})
	if err != nil {
		t.Fatal(err)
	}
	_, commitsBefore, _, _ := transport.observed()
	result, err := connector.ExecuteAction(ctx, runtime, prepared)
	if err != nil || result.Status != connectors.ResultFailed || !strings.Contains(result.Error, "must be inactive") {
		t.Fatalf("active group admission: %#v, %v", result, err)
	}
	_, commitsAfter, _, invalid := transport.observed()
	if commitsAfter != commitsBefore || invalid {
		t.Fatal("active group rejection dispatched an offset mutation")
	}
	after, err := admin.FetchOffsets(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	offset, ok := after.Lookup(topic, 1)
	if !ok || offset.At != 1 || offset.Err != nil {
		t.Fatalf("active group offset changed: %#v", offset)
	}
}
