package actions

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestTerminalPersistenceResponsePreservesSafeExactIdentity(t *testing.T) {
	for _, requestID := range []int64{42, 9007199254740993} {
		t.Run(fmt.Sprint(requestID), func(t *testing.T) {
			failure := &TerminalPersistenceError{RequestID: requestID, Err: errors.New("private diagnostic cause")}
			encoded, err := json.Marshal(failure.Response())
			if err != nil {
				t.Fatal(err)
			}
			var actual map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &actual); err != nil {
				t.Fatal(err)
			}
			want := map[string]json.RawMessage{
				"status": json.RawMessage(`"outcome_unknown"`), "code": json.RawMessage(`"connector_action_persistence_unknown"`),
				"request_id":     json.RawMessage(fmt.Sprint(requestID)),
				"error":          json.RawMessage(`"connector action may have been dispatched, but its final state could not be persisted; inspect request history before retrying"`),
				"assistant_hint": json.RawMessage(`"Do not retry automatically. Inspect the recorded request and external target state first."`),
			}
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("unsafe or incomplete persistence response: %s", encoded)
			}
		})
	}
}
