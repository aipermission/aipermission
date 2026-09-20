package kafka

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestPublishMessageSchemaPreservesWhitespaceOnlyPayloads(t *testing.T) {
	action, ok := actionDefinition(ActionPublishMessage)
	if !ok {
		t.Fatal("publish action not found")
	}
	normalized, err := connectors.NormalizeSchemaValues(action.InputSchema, map[string]any{
		"topic": "events", "partition": 0,
		"key": " \t", "key_encoding": "utf8",
		"value": "\n ", "value_encoding": "utf8", "headers": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if normalized["key"] != " \t" || normalized["value"] != "\n " {
		t.Fatalf("normalized payload = %#v", normalized)
	}
}

func TestPublishMessagePreservesExactKeyValueAndPreviewLengths(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.SeedTopics(1, "identity-events"))
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.Close()
	runtime := testRuntime(cluster.ListenAddrs(), &recordingDirectTransport{})

	tests := []struct {
		name          string
		key           string
		keyEncoding   string
		value         string
		valueEncoding string
		wantKey       []byte
		wantValue     []byte
	}{
		{name: "utf8 padded", key: " record-key ", keyEncoding: "utf8", value: " line one\n", valueEncoding: "utf8", wantKey: []byte(" record-key "), wantValue: []byte(" line one\n")},
		{name: "utf8 whitespace only", key: " \t", keyEncoding: "utf8", value: "\n ", valueEncoding: "utf8", wantKey: []byte(" \t"), wantValue: []byte("\n ")},
		{name: "utf8 empty", key: "", keyEncoding: "utf8", value: "", valueEncoding: "utf8", wantKey: []byte{}, wantValue: []byte{}},
		{name: "base64", key: base64.StdEncoding.EncodeToString([]byte(" key\n")), keyEncoding: "base64", value: base64.StdEncoding.EncodeToString([]byte(" value \n")), valueEncoding: "base64", wantKey: []byte(" key\n"), wantValue: []byte(" value \n")},
	}

	for _, test := range tests {
		request := connectors.ActionRequest{
			Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionPublishMessage,
			Input: map[string]any{
				"topic": "identity-events", "partition": 0,
				"key": test.key, "key_encoding": test.keyEncoding,
				"value": test.value, "value_encoding": test.valueEncoding,
				"headers": []any{map[string]any{"key": " trace-id ", "value": " header value ", "encoding": "utf8"}},
			},
		}
		prepared, err := New().PrepareAction(context.Background(), request)
		if err != nil {
			t.Fatalf("%s prepare: %v", test.name, err)
		}
		if prepared.Payload["key"] != test.key || prepared.Payload["value"] != test.value {
			t.Fatalf("%s prepared payload changed: %#v", test.name, prepared.Payload)
		}
		if prepared.Preview["key_bytes"] != len(test.wantKey) || prepared.Preview["value_bytes"] != len(test.wantValue) {
			t.Fatalf("%s preview = %#v", test.name, prepared.Preview)
		}
		result, err := New().ExecuteAction(context.Background(), runtime, prepared)
		if err != nil || result.Status != connectors.ResultCompleted {
			t.Fatalf("%s publish: %#v, %v", test.name, result, err)
		}
	}

	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(cluster.ListenAddrs()...),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{"identity-events": {0: kgo.NewOffset().At(0)}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	records := []*kgo.Record{}
	for len(records) < len(tests) && ctx.Err() == nil {
		fetches := consumer.PollFetches(ctx)
		if err := fetches.Err(); err != nil {
			t.Fatal(err)
		}
		records = append(records, fetches.Records()...)
	}
	if len(records) != len(tests) {
		t.Fatalf("records = %d, want %d", len(records), len(tests))
	}
	for index, test := range tests {
		if !bytes.Equal(records[index].Key, test.wantKey) || !bytes.Equal(records[index].Value, test.wantValue) {
			t.Fatalf("%s record changed: key=%q value=%q", test.name, records[index].Key, records[index].Value)
		}
		if len(records[index].Headers) != 1 || records[index].Headers[0].Key != " trace-id " || !bytes.Equal(records[index].Headers[0].Value, []byte(" header value ")) {
			t.Fatalf("%s header changed: %#v", test.name, records[index].Headers)
		}
	}
}
