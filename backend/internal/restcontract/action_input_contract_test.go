package restcontract

import (
	"encoding/json"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestActionInputBudgetMatchesWireAndCanonicalBounds(t *testing.T) {
	schema := sharedSchemas()["ConnectorActionDefinition"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	budget, ok := properties["max_input_bytes"].(map[string]any)
	if !ok {
		t.Fatal("published action input budget is missing from the REST contract")
	}
	if budget["type"] != "integer" || budget["minimum"] != 1 || budget["maximum"] != connectors.MaximumActionInputBytes {
		t.Fatalf("action input budget constraints=%#v", budget)
	}
	required := schema["required"].([]string)
	found := false
	for _, field := range required {
		found = found || field == "max_input_bytes"
	}
	if !found {
		t.Fatal("action input budget must be required")
	}
	for _, limit := range []int{1, connectors.DefaultMaxActionInputBytes, connectors.MaximumActionInputBytes} {
		wire, err := json.Marshal(connectors.ActionDefinition{MaxInputBytes: limit})
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(wire, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["max_input_bytes"] != float64(limit) {
			t.Fatalf("wire budget=%#v, want %d", decoded["max_input_bytes"], limit)
		}
	}
}
