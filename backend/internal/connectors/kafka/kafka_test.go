package kafka

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/klauspost/compress/s2"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestTargetAndCredentialSchemasExposeKafkaRedpandaAndSASL(t *testing.T) {
	target := New().TargetSchema()
	if len(target.Fields) == 0 || target.Fields[0].Name != "server_family" {
		t.Fatalf("server family field missing: %#v", target.Fields)
	}
	if !reflect.DeepEqual(target.Fields[0].Options, []connectors.FieldOption{
		{Value: "kafka", Label: "Apache Kafka"},
		{Value: "redpanda", Label: "Redpanda"},
	}) {
		t.Fatalf("server family options = %#v", target.Fields[0].Options)
	}
	credentials := New().CredentialSchemas()
	if len(credentials) != 1 || credentials[0].Kind != "sasl" {
		t.Fatalf("credential schemas = %#v", credentials)
	}
	if !connectors.SchemaContainsSecret(credentials[0].Schema) {
		t.Fatal("SASL password must be a secret field")
	}
}

func TestPrepareActionNormalizesAndBoundsMessageReads(t *testing.T) {
	request := connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "kafka:1:2", Config: map[string]any{"server_family": "redpanda"}},
		Profile:    connectors.CredentialProfileView{ID: 2, Public: map[string]any{"mechanism": "none"}},
		ActionName: ActionReadMessages,
		Input: map[string]any{
			"topic":          "events",
			"partition":      2,
			"start_position": "recent",
			"max_records":    100,
			"max_bytes":      1048576,
			"wait_seconds":   10,
		},
	}
	prepared, err := New().PrepareAction(context.Background(), request)
	if err != nil {
		t.Fatalf("prepare action: %v", err)
	}
	if prepared.Risk != connectors.RiskRead || prepared.Payload["topic"] != "events" {
		t.Fatalf("prepared = %#v", prepared)
	}
	if prepared.Payload["offset"] != "0" || prepared.Payload["partition"] != 2 {
		t.Fatalf("prepared numeric values are not canonical: %#v", prepared.Payload)
	}

	request.Input["max_records"] = 101
	if _, err := New().PrepareAction(context.Background(), request); err == nil {
		t.Fatal("expected max_records bound error")
	}
	request.Input["max_records"] = 20.5
	if _, err := New().PrepareAction(context.Background(), request); err == nil || !strings.Contains(err.Error(), "exact integer") {
		t.Fatalf("expected exact integer error, got %v", err)
	}
	request.Input["max_records"] = 20
	request.Input["start_position"] = "offset"
	request.Input["offset"] = "9223372036854775806"
	request.Input["topic"] = "  events  "
	prepared, err = New().PrepareAction(context.Background(), request)
	if err != nil {
		t.Fatalf("prepare exact offset: %v", err)
	}
	if prepared.Payload["offset"] != "9223372036854775806" || prepared.Payload["topic"] != "events" {
		t.Fatalf("exact offset or identifier drifted: %#v", prepared.Payload)
	}
	request.Input["start_position"] = "recent"
	request.Input["offset"] = ""
	prepared, err = New().PrepareAction(context.Background(), request)
	if err != nil {
		t.Fatalf("prepare non-offset read with empty offset: %v", err)
	}
	if prepared.Payload["offset"] != "0" {
		t.Fatalf("non-offset read should canonicalize offset to zero: %#v", prepared.Payload)
	}
}

func TestKafkaIntegerConversionsRejectNarrowingOverflow(t *testing.T) {
	if _, err := exactInt("9223372036854775807", "max_bytes"); strconv.IntSize == 32 && err == nil {
		t.Fatal("expected platform integer overflow error")
	}
	if _, err := checkedInt32(1<<31, "partition"); err == nil {
		t.Fatal("expected 32-bit overflow error")
	}
	if value, err := checkedInt32(1_048_576, "max_bytes"); err != nil || value != 1_048_576 {
		t.Fatalf("checked int32 = %d, %v", value, err)
	}
}

func TestKafkaHelpUsesSharedConnectorIdentityFields(t *testing.T) {
	help, err := New().GetHelp(context.Background(), connectors.TargetView{Ref: "kafka:1:2"})
	if err != nil {
		t.Fatalf("get help: %v", err)
	}
	if help.Connector != Label || help.ConnectorID != Kind {
		t.Fatalf("help connector identity = %#v", help)
	}
}

func TestKafkaCredentialValidationRequiresPasswordWhenEnablingSASL(t *testing.T) {
	connector := New()
	public := map[string]any{"mechanism": "scram_sha_256", "username": "reader"}
	if err := connector.ValidateCredentialProfile("sasl", public, nil, nil); err == nil {
		t.Fatal("expected password requirement on create")
	}
	previousNone := &connectors.CredentialProfileView{Public: map[string]any{"mechanism": "none"}}
	if err := connector.ValidateCredentialProfile("sasl", public, nil, previousNone); err == nil {
		t.Fatal("expected password requirement when enabling SASL")
	}
	previousSASL := &connectors.CredentialProfileView{Public: map[string]any{"mechanism": "scram_sha_512"}}
	if err := connector.ValidateCredentialProfile("sasl", public, nil, previousSASL); err != nil {
		t.Fatalf("preserving an existing SASL password should be valid: %v", err)
	}
}

func TestPlainSASLRequiresTLSOrExplicitOptIn(t *testing.T) {
	runtime := testRuntime([]string{"127.0.0.1:9092"}, &recordingDirectTransport{})
	runtime.Profile.Public = map[string]any{"mechanism": "plain", "username": "reader"}
	runtime.Secrets = staticSecretAccessor{"password": "test-password"}
	if _, err := parseClientConfig(context.Background(), runtime); err == nil || !strings.Contains(err.Error(), "requires TLS") {
		t.Fatalf("expected TLS requirement, got %v", err)
	}
	runtime.Target.Config["allow_insecure_plain_sasl"] = true
	if _, err := parseClientConfig(context.Background(), runtime); err != nil {
		t.Fatalf("explicit insecure PLAIN opt-in should be accepted: %v", err)
	}
}

func TestBoundedDecompressorRejectsExpandedBatch(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(bytes.Repeat([]byte("x"), 4096)); err != nil {
		t.Fatalf("compress fixture: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close compressor: %v", err)
	}
	_, err := (boundedDecompressor{maxBytes: 1024}).Decompress(compressed.Bytes(), kgo.CodecGzip)
	if err == nil || !strings.Contains(err.Error(), "decompression limit") {
		t.Fatalf("expected bounded decompression error, got %v", err)
	}
}

func TestBoundedDecompressorReadsXerialSnappyAndRejectsMalformedChunks(t *testing.T) {
	source := []byte("bounded xerial snappy payload")
	compressed := s2.EncodeSnappy(nil, source)
	framed := append([]byte{}, xerialSnappyPrefix...)
	framed = append(framed, 0, 0, 0, 1, 0, 0, 0, 1)
	size := make([]byte, 4)
	binary.BigEndian.PutUint32(size, uint32(len(compressed)))
	framed = append(framed, size...)
	framed = append(framed, compressed...)

	decoded, err := (boundedDecompressor{maxBytes: 1024}).Decompress(framed, kgo.CodecSnappy)
	if err != nil {
		t.Fatalf("decode xerial snappy: %v", err)
	}
	if !bytes.Equal(decoded, source) {
		t.Fatalf("decoded xerial payload = %q", decoded)
	}

	malformed := append([]byte{}, xerialSnappyPrefix...)
	malformed = append(malformed, 0, 0, 0, 1, 0, 0, 0, 1)
	binary.BigEndian.PutUint32(size, uint32(len(compressed)+1))
	malformed = append(malformed, size...)
	malformed = append(malformed, compressed...)
	if _, err := (boundedDecompressor{maxBytes: 1024}).Decompress(malformed, kgo.CodecSnappy); err == nil || !strings.Contains(err.Error(), "malformed xerial") {
		t.Fatalf("expected malformed xerial chunk error, got %v", err)
	}
}

func TestDisplayBytesDistinguishesTombstonesFromEmptyValues(t *testing.T) {
	value, encoding := displayBytes(nil)
	if value != nil || encoding != "null" {
		t.Fatalf("tombstone = %#v %q", value, encoding)
	}
	value, encoding = displayBytes([]byte{})
	if value != "" || encoding != "utf8" {
		t.Fatalf("empty value = %#v %q", value, encoding)
	}
}

func TestReadMessageResultDoesNotTruncateAtSnapshotBoundary(t *testing.T) {
	req := readMessagesRequest{Topic: "events", Partition: 0, MaxRecords: 10, MaxBytes: 1024}
	result := buildReadMessagesResult(req, 0, 1, []*kgo.Record{
		{Topic: "events", Partition: 0, Offset: 0, Value: []byte("inside")},
		{Topic: "events", Partition: 0, Offset: 1, Value: []byte("after-snapshot")},
	})
	if result.Status != connectors.ResultCompleted {
		t.Fatalf("result = %#v", result)
	}
	output := result.Output.(map[string]any)
	if output["truncated"] != false || output["count"] != 1 {
		t.Fatalf("snapshot-bounded output = %#v", output)
	}
	if _, exists := output["continuation_offset"]; exists {
		t.Fatalf("snapshot boundary should not expose continuation: %#v", output)
	}
}

func TestReadMessageResultFailsWhenOneRecordCannotAdvanceByteBound(t *testing.T) {
	req := readMessagesRequest{Topic: "events", Partition: 0, MaxRecords: 10, MaxBytes: 3}
	result := buildReadMessagesResult(req, 0, 1, []*kgo.Record{
		{Topic: "events", Partition: 0, Offset: 0, Value: []byte("four")},
	})
	if result.Status != connectors.ResultFailed || !strings.Contains(result.Error, "increase max_bytes") {
		t.Fatalf("oversized record result = %#v", result)
	}
}

func TestReadTimeoutIsAnEmptyCompletedResult(t *testing.T) {
	req := readMessagesRequest{Topic: "events", Partition: 2, WaitSeconds: 3}
	result := completedReadTimeout(req, 5, 9)
	if result.Status != connectors.ResultCompleted {
		t.Fatalf("timeout result = %#v", result)
	}
	output := result.Output.(map[string]any)
	if output["timed_out"] != true || output["count"] != 0 || output["truncated"] != false {
		t.Fatalf("timeout output = %#v", output)
	}
}

func TestParseBrokerListRequiresExplicitPortsAndDeduplicates(t *testing.T) {
	brokers, err := parseBrokerList("broker-a:9092,\nbroker-a:9092 broker-b:9093")
	if err != nil {
		t.Fatalf("parse brokers: %v", err)
	}
	if !reflect.DeepEqual(brokers, []string{"broker-a:9092", "broker-b:9093"}) {
		t.Fatalf("brokers = %#v", brokers)
	}
	if _, err := parseBrokerList("broker-a"); err == nil {
		t.Fatal("expected missing port error")
	}
}

func TestReadActionsUseGenericTransportAndDoNotCommitOffsets(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.NumBrokers(2), kfake.SeedTopics(2, "events"))
	if err != nil {
		t.Fatalf("new fake cluster: %v", err)
	}
	defer cluster.Close()

	producer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...), kgo.RecordPartitioner(kgo.ManualPartitioner()))
	if err != nil {
		t.Fatalf("new producer: %v", err)
	}
	results := producer.ProduceSync(context.Background(),
		&kgo.Record{Topic: "events", Partition: 0, Key: []byte("one"), Value: []byte(`{"ok":1}`)},
		&kgo.Record{Topic: "events", Partition: 0, Key: []byte{0xff}, Value: []byte{0x00, 0xff}},
	)
	producer.Close()
	if err := results.FirstErr(); err != nil {
		t.Fatalf("produce fixtures: %v", err)
	}

	transport := &recordingDirectTransport{}
	runtime := testRuntime(cluster.ListenAddrs(), transport)
	result, err := New().ExecuteAction(context.Background(), runtime, connectors.PreparedAction{
		ConnectorKind: Kind,
		ActionName:    ActionReadMessages,
		Payload: map[string]any{
			"topic": "events", "partition": 0, "start_position": "earliest",
			"offset": "0", "max_records": 10, "max_bytes": 262144, "wait_seconds": 2,
		},
	})
	if err != nil {
		t.Fatalf("execute read: %v", err)
	}
	if result.Status != connectors.ResultCompleted {
		t.Fatalf("result = %#v", result)
	}
	output := result.Output.(map[string]any)
	records, ok := output["records"].([]map[string]any)
	if !ok || len(records) != 2 || records[1]["value_encoding"] != "base64" {
		t.Fatalf("records = %#v", records)
	}
	if len(transport.Requests()) < 2 {
		t.Fatalf("expected bootstrap and metadata broker dials, got %#v", transport.Requests())
	}

	consumer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...))
	if err != nil {
		t.Fatalf("new consumer: %v", err)
	}
	defer consumer.Close()
	committed, _ := kadm.NewClient(consumer).FetchOffsets(context.Background(), "audit-check")
	if len(committed) != 0 {
		t.Fatalf("read action unexpectedly committed offsets: %#v", committed)
	}
}

func TestConnectionAndTopicMetadataAgainstKafkaProtocol(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.ClusterID("aipermission-test"), kfake.SeedTopics(3, "orders"))
	if err != nil {
		t.Fatalf("new fake cluster: %v", err)
	}
	defer cluster.Close()
	runtime := testRuntime(cluster.ListenAddrs(), &recordingDirectTransport{})

	testResult, err := New().TestConnection(context.Background(), runtime)
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if testResult.Status != connectors.TestOK || testResult.Details["cluster_id"] != "aipermission-test" {
		t.Fatalf("test result = %#v", testResult)
	}
	result, err := New().ExecuteAction(context.Background(), runtime, connectors.PreparedAction{
		ConnectorKind: Kind,
		ActionName:    ActionDescribeTopic,
		Payload:       map[string]any{"topic": "orders"},
	})
	if err != nil {
		t.Fatalf("describe topic: %v", err)
	}
	output := result.Output.(map[string]any)
	if output["partition_count"] != 3 {
		t.Fatalf("output = %#v", output)
	}
}

func testRuntime(brokers []string, transport connectors.NetworkTransport) connectors.RuntimeContext {
	return connectors.RuntimeContext{
		Target: connectors.TargetView{
			ID: 1, Ref: "kafka:1:2", ConnectorKind: Kind, Name: "events",
			Config: map[string]any{
				"server_family": "redpanda", "connection_mode": "direct",
				"bootstrap_brokers": brokers[0], "tls_enabled": false,
			},
		},
		Profile: connectors.CredentialProfileView{
			ID: 2, TargetID: 1, ConnectorKind: Kind, Kind: "sasl", Label: "monitor",
			Public: map[string]any{"mechanism": "none"},
		},
		Capabilities: testCapabilities{transport: transport},
	}
}

type testCapabilities struct{ transport connectors.NetworkTransport }

type staticSecretAccessor map[string]string

func (s staticSecretAccessor) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := s[name]
	if !ok {
		return "", fmt.Errorf("secret %s not found", name)
	}
	return value, nil
}

func (c testCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.NetworkTransportCapabilityName {
		return c.transport
	}
	return nil
}

type recordingDirectTransport struct {
	mu       sync.Mutex
	requests []connectors.NetworkDialRequest
}

func (*recordingDirectTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (t *recordingDirectTransport) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	t.mu.Lock()
	t.requests = append(t.requests, request)
	t.mu.Unlock()
	return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(request.Host, fmt.Sprint(request.Port)))
}

func (t *recordingDirectTransport) Requests() []connectors.NetworkDialRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]connectors.NetworkDialRequest(nil), t.requests...)
}
