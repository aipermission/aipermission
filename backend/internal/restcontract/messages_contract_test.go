package restcontract

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/messagequeue"
)

func TestMessageAcknowledgementSchemaMatchesBoundedExactSelection(t *testing.T) {
	contract, exists := typedOperationContracts()[Route{Method: "POST", Path: "/api/messages/read"}]
	if !exists || contract.StatusCode != "200" || contract.AdditionalResponses["400"] == nil {
		t.Fatalf("message mutation lost typed success/invalid response: %#v", contract)
	}
	request := contract.RequestSchema
	if request["additionalProperties"] != false || len(request["required"].([]string)) != 2 {
		t.Fatalf("message mutation lost required selection: %#v", request)
	}
	fields := request["properties"].(map[string]any)
	selection := fields["message_ids"].(map[string]any)
	if selection["minItems"] != 1 || selection["maxItems"] != messagequeue.MaxReadMessages || selection["uniqueItems"] != true {
		t.Fatalf("message selection lost limits: %#v", selection)
	}
	for _, field := range []map[string]any{fields["runtime_id"].(map[string]any), selection["items"].(map[string]any)} {
		if field["type"] != "integer" || field["minimum"] != 1 {
			t.Fatalf("message selection lost exact positive IDs: %#v", field)
		}
	}
}
