package kafka

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestConsumerGroupIdentitySurvivesPrepareAndJSON(t *testing.T) {
	for _, group := range []string{" workers ", "\tworkers\n", " ", "\u00a0workers\u00a0"} {
		for _, action := range []string{ActionDescribeConsumerGroup, ActionSetConsumerGroupOffset} {
			input := map[string]any{"group": group}
			if action == ActionSetConsumerGroupOffset {
				input["topic"], input["partition"], input["offset"] = "events", 0, "1"
			}
			prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{ActionName: action, Input: input})
			if err != nil {
				t.Fatalf("prepare %s group %q: %v", action, group, err)
			}
			encoded, err := json.Marshal(prepared)
			if err != nil {
				t.Fatal(err)
			}
			var restored connectors.PreparedAction
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			if restored.Payload["group"] != group {
				t.Fatalf("%s group changed: want %q, got %#v", action, group, restored.Payload["group"])
			}
		}
	}
}

func TestConsumerGroupOffsetDoesNotMutateWhitespaceNeighbor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cluster, err := kfake.NewCluster(kfake.SeedTopics(1, "events"))
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.Close()
	client, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.ProduceSync(ctx, &kgo.Record{Topic: "events", Value: []byte("one")}).FirstErr(); err != nil {
		t.Fatal(err)
	}
	admin := kadm.NewClient(client)
	initial := kadm.Offsets{}
	initial.Add(kadm.Offset{Topic: "events", Partition: 0, At: 0})
	for _, group := range []string{" workers ", "workers"} {
		if err := admin.CommitAllOffsets(ctx, group, initial); err != nil {
			t.Fatal(err)
		}
	}
	connector := New()
	prepared, err := connector.PrepareAction(ctx, connectors.ActionRequest{
		ActionName: ActionSetConsumerGroupOffset,
		Input:      map[string]any{"group": " workers ", "topic": "events", "partition": 0, "offset": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &prepared); err != nil {
		t.Fatal(err)
	}
	result, err := connector.ExecuteAction(ctx, testRuntime(cluster.ListenAddrs(), &recordingDirectTransport{}), prepared)
	if err != nil || result.Status != connectors.ResultCompleted {
		t.Fatalf("execute exact group: result=%#v error=%v", result, err)
	}
	if result.Output.(map[string]any)["group"] != " workers " {
		t.Fatalf("output changed group identity: %#v", result.Output)
	}
	for group, want := range map[string]int64{" workers ": 1, "workers": 0} {
		offsets, err := admin.FetchOffsets(ctx, group)
		if err != nil {
			t.Fatal(err)
		}
		actual, found := offsets.Lookup("events", 0)
		if !found || actual.At != want {
			t.Fatalf("group %q offset=%#v; want %d", group, actual, want)
		}
	}
}
