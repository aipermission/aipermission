package connectortargets

import (
	"encoding/json"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestStoredActionOutputPreservesPublicNumbers(t *testing.T) {
	input := `{"rows":[{"big":9007199254740993,"min":-9223372036854775808,"decimal":0.10000000000000000001,"safe":42,"fraction":0.1}]}`
	got, err := parseJSONValue(input)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"rows":[{"big":"9007199254740993","decimal":"0.10000000000000000001","fraction":0.1,"min":"-9223372036854775808","safe":42}]}`
	if string(encoded) != want {
		t.Fatalf("stored output changed numeric data: %s", encoded)
	}
	replayed, err := parseJSONValue(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	replayJSON, err := jsonValueString(replayed)
	if err != nil || replayJSON != want {
		t.Fatalf("replay changed numeric data: %s %v", replayJSON, err)
	}
}

func TestActionResultNumbersSurvivePersistenceAndIdempotentReplay(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, t.Context(), store)
	input := InsertActionRequestInput{
		TokenID: &tokenID, TargetID: target.ID, ProfileID: profile.ID,
		ConnectorKind: "postgres", ActionName: "query_readonly", Input: map[string]any{"sql": "select 1"},
		Status: connectors.ResultRunning, EncryptedPayloadJSON: "encrypted", ApprovalContext: `{}`, ApprovalContextHash: "approval-hash",
		IdempotencyKey: "numeric-result", IdempotencyIdentityHash: "numeric-identity",
	}
	request, created, err := store.InsertActionRequestIdempotent(t.Context(), input)
	if err != nil || !created {
		t.Fatalf("insert: %v created=%v", err, created)
	}
	output := map[string]any{"big": int64(9007199254740993), "decimal": json.Number("0.10000000000000000001"), "small": 42}
	finished, err := store.FinishActionRequest(t.Context(), FinishActionRequestInput{ID: request.ID, Status: connectors.ResultCompleted, Output: output})
	if err != nil {
		t.Fatal(err)
	}
	replayed, created, err := store.InsertActionRequestIdempotent(t.Context(), input)
	if err != nil || created || replayed.ID != request.ID {
		t.Fatalf("replay: %v created=%v", err, created)
	}
	for _, value := range []any{finished.Output, replayed.Output} {
		encoded, err := json.Marshal(value)
		if err != nil || string(encoded) != `{"big":"9007199254740993","decimal":"0.10000000000000000001","small":42}` {
			t.Fatalf("persistent result rounded: %s %v", encoded, err)
		}
	}
}
