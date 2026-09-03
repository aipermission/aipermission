package connectors

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func FuzzNormalizeSchemaValues(f *testing.F) {
	f.Add([]byte(`{"name":"worker","port":"5432","enabled":true,"mode":"safe","options":{"limit":10}}`))
	f.Add([]byte(`{"name":"worker","port":5432,"enabled":false,"mode":"fast","options":[1,2,3]}`))
	f.Add([]byte(`{"unknown":"field"}`))
	f.Add([]byte(`{"name":"worker","port":"NaN"}`))
	f.Add([]byte(`{"name":"worker","port":"Inf"}`))
	f.Add([]byte(`{"name":"worker","port":1.5}`))
	f.Add([]byte(`{"name":"worker","port":9223372036854775808}`))
	f.Add([]byte(`null`))

	schema := Schema{Fields: []Field{
		{Name: "name", Type: FieldString, Required: true},
		{Name: "port", Type: FieldInteger, Default: 5432},
		{Name: "enabled", Type: FieldBoolean, Default: false},
		{Name: "mode", Type: FieldSelect, Default: "safe", Options: []FieldOption{{Value: "safe", Label: "Safe"}, {Value: "fast", Label: "Fast"}}},
		{Name: "options", Type: FieldJSON},
	}}

	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 128<<10 {
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.UseNumber()
		var values map[string]any
		if err := decoder.Decode(&values); err != nil {
			return
		}
		normalized, err := NormalizeSchemaValues(schema, values)
		if err != nil {
			return
		}
		repeated, err := NormalizeSchemaValues(schema, normalized)
		if err != nil {
			t.Fatalf("normalized connector payload was rejected: %v", err)
		}
		if !reflect.DeepEqual(normalized, repeated) {
			t.Fatalf("connector payload normalization is not idempotent: first=%#v second=%#v", normalized, repeated)
		}
		encoded, err := json.Marshal(normalized)
		if err != nil {
			t.Fatalf("marshal normalized connector payload: %v", err)
		}
		var roundTrip map[string]any
		roundTripDecoder := json.NewDecoder(bytes.NewReader(encoded))
		roundTripDecoder.UseNumber()
		if err := roundTripDecoder.Decode(&roundTrip); err != nil {
			t.Fatalf("round-trip normalized connector payload: %v", err)
		}
		if _, err := NormalizeSchemaValues(schema, roundTrip); err != nil {
			t.Fatalf("round-trip connector payload was rejected: %v", err)
		}
	})
}
