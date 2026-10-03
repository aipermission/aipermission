package restcontract

import "github.com/aipermission/aipermission/backend/internal/messagequeue"

func messageReadContract() operationContract {
	positiveID := integerSchema()
	positiveID["minimum"] = 1
	selection := arraySchema(positiveID)
	selection["minItems"] = 1
	selection["maxItems"] = messagequeue.MaxReadMessages
	selection["uniqueItems"] = true
	count := integerSchema()
	count["minimum"] = 0
	count["maximum"] = messagequeue.MaxReadMessages
	return operationContract{
		StatusCode: "200",
		RequestSchema: objectSchema(map[string]any{
			"runtime_id": positiveID, "message_ids": selection,
		}, []string{"runtime_id", "message_ids"}),
		ResponseSchema: objectSchema(map[string]any{
			"status": enumSchema("read"), "count": count,
		}, []string{"status", "count"}),
		AdditionalResponses: map[string]map[string]any{"400": refSchema("Error")},
	}
}
