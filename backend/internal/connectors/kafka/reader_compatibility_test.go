package kafka

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/pierrec/lz4/v4"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func TestReadMessagesDoesNotResetAnInvalidatedExplicitOffset(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.SeedTopics(1, "events"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	producer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...))
	if err != nil {
		t.Fatal(err)
	}
	if err := producer.ProduceSync(ctx,
		&kgo.Record{Topic: "events", Value: []byte("before")},
		&kgo.Record{Topic: "events", Value: []byte("selected")},
	).FirstErr(); err != nil {
		producer.Close()
		t.Fatal(err)
	}
	producer.Close()
	// Invalidate the selected offset after metadata validation, on the first fetch.
	cluster.ControlKey(int16(kmsg.Fetch), func(request kmsg.Request) (kmsg.Response, error, bool) {
		fetch := request.(*kmsg.FetchRequest)
		response := fetch.ResponseKind().(*kmsg.FetchResponse)
		for _, topic := range fetch.Topics {
			row := kmsg.NewFetchResponseTopic()
			row.Topic, row.TopicID = topic.Topic, topic.TopicID
			for _, partition := range topic.Partitions {
				part := kmsg.NewFetchResponseTopicPartition()
				part.Partition = partition.Partition
				part.ErrorCode = kerr.OffsetOutOfRange.Code
				row.Partitions = append(row.Partitions, part)
			}
			response.Topics = append(response.Topics, row)
		}
		return response, nil, true
	})
	result, err := executeReadMessages(ctx, testRuntime(cluster.ListenAddrs(), &recordingDirectTransport{}), readMessagesRequest{
		Topic: "events", Partition: 0, Start: "offset", Offset: 1,
		MaxRecords: 10, MaxBytes: 262144, WaitSeconds: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != connectors.ResultFailed || !strings.Contains(result.Error, "OFFSET_OUT_OF_RANGE") {
		t.Fatalf("invalidated explicit offset silently reset: %#v", result)
	}
}

func TestBoundedLZ4DecompressionPreservesPayloadAndLimit(t *testing.T) {
	for _, size := range []int{0, 1024, 1025} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			source := bytes.Repeat([]byte("x"), size)
			var compressed bytes.Buffer
			writer := lz4.NewWriter(&compressed)
			if _, err := writer.Write(source); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			decoded, err := (boundedDecompressor{maxBytes: 1024}).Decompress(compressed.Bytes(), kgo.CodecLz4)
			if size > 1024 {
				if err == nil || !strings.Contains(err.Error(), "decompression limit") {
					t.Fatalf("expanded LZ4 batch accepted: %v", err)
				}
				return
			}
			if err != nil || !bytes.Equal(decoded, source) {
				t.Fatalf("LZ4 roundtrip changed payload: len=%d err=%v", len(decoded), err)
			}
		})
	}
}
