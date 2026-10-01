package redisconnector

import (
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestCollectionContinuationUsesSharedActionSchemaDefaults(t *testing.T) {
	actions, err := (Connector{}).GetActionList(t.Context(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatal(err)
	}
	var definition connectors.ActionDefinition
	for _, action := range actions {
		if action.Name == ActionGetKey {
			definition = action
		}
	}
	if definition.OutputHint.MaxBytes != maxKeyPreviewEncodedBytes {
		t.Fatalf("output metadata does not advertise the complete preview budget: %#v", definition.OutputHint)
	}
	for _, input := range []map[string]any{
		{"key": "key"},
		{"key": "key", "cursor": nil, "offset": nil},
		{"key": "key", "cursor": "", "offset": 0},
		{"key": "key", "cursor": "7", "offset": 2},
	} {
		normalized, err := connectors.NormalizeSchemaValues(definition.InputSchema, input)
		if err != nil {
			t.Fatal(err)
		}
		direct, err := collectionPreviewPosition(input)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := collectionPreviewPosition(normalized)
		if err != nil || !reflect.DeepEqual(direct, canonical) {
			t.Fatalf("shared schema changes continuation: direct=%#v canonical=%#v err=%v", direct, canonical, err)
		}
		if _, err := (Connector{}).PrepareAction(t.Context(), connectors.ActionRequest{ActionName: ActionGetKey, Input: normalized}); err != nil {
			t.Fatal(err)
		}
	}
}
