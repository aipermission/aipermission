package connectors

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGetActionDefinitionsPublishesEffectiveInputBudget(t *testing.T) {
	for _, configured := range []int{0, 1, MaximumActionInputBytes} {
		connector := retryTestConnector{actions: []ActionDefinition{{
			Name: "read", Label: "Read", Description: "Read state", Risk: RiskRead, MaxInputBytes: configured,
		}}}
		actions, err := GetActionDefinitions(context.Background(), connector, TargetView{}, CredentialProfileView{})
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(actions)
		if err != nil {
			t.Fatal(err)
		}
		var wire []map[string]any
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		want := configured
		if want == 0 {
			want = DefaultMaxActionInputBytes
		}
		if len(wire) != 1 || wire[0]["max_input_bytes"] != float64(want) {
			t.Fatalf("published input budget=%s, want %d", data, want)
		}
		if connector.actions[0].MaxInputBytes != configured {
			t.Fatal("publishing mutated connector-owned input budget")
		}
	}
}
