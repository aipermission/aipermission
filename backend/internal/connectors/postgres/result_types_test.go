package postgresconnector

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPostgresNativeCodecsKeepInfinityAndNestedNumbers(t *testing.T) {
	types := pgtype.NewMap()
	configurePostgresTypes(types)
	for _, tc := range []struct {
		oid  uint32
		wire string
		want any
	}{
		{pgtype.DateOID, "infinity", "infinity"},
		{pgtype.TimestampOID, "-infinity", "-infinity"},
		{pgtype.TimestamptzOID, "infinity", "infinity"},
		{pgtype.DateArrayOID, "{infinity,-infinity}", []any{"infinity", "-infinity"}},
		{pgtype.JSONOID, `{"n":9007199254740993,"decimal":12345678901234567890.123456789,"safe":42}`, map[string]any{"n": "9007199254740993", "decimal": "12345678901234567890.123456789", "safe": json.Number("42")}},
		{pgtype.JSONBOID, `{"nested":[9007199254740993]}`, map[string]any{"nested": []any{"9007199254740993"}}},
		{pgtype.JSONArrayOID, `{"9007199254740993"}`, []any{"9007199254740993"}},
		{pgtype.JSONBArrayOID, `{"9007199254740993"}`, []any{"9007199254740993"}},
	} {
		t.Run(tc.wire, func(t *testing.T) {
			kind, _ := types.TypeForOID(tc.oid)
			value, err := kind.Codec.DecodeValue(types, tc.oid, pgtype.TextFormatCode, []byte(tc.wire))
			if err != nil {
				t.Fatal(err)
			}
			projected, err := actionresult.Canonicalize(normalizePostgresValue(value), actionresult.DefaultLimits())
			if err != nil || !reflect.DeepEqual(projected, tc.want) {
				t.Fatalf("native value projected as %#v, want %#v: %v", projected, tc.want, err)
			}
		})
	}
}

func TestPostgresJSONDecoderRejectsTrailingValuesAndSupportsTypedDestinations(t *testing.T) {
	for _, text := range []string{`1 2`, `{} trailing`, `{"a":`, ``} {
		var value any
		if err := decodePostgresJSON([]byte(text), &value); err == nil {
			t.Fatalf("accepted invalid JSON %q", text)
		}
	}
	var typed struct {
		N int `json:"n"`
	}
	if err := decodePostgresJSON([]byte(`{"n":42}`), &typed); err != nil || typed.N != 42 {
		t.Fatalf("typed JSON scan changed: %#v %v", typed, err)
	}
}
