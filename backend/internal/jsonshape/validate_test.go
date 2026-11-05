package jsonshape

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMeasureCanonicalStructure(t *testing.T) {
	limits := Limits{Depth: 3, Nodes: 6, StringBytes: 3}
	value := map[string]any{"a": []any{json.Number("1"), true, nil, "ok"}}
	if nodes, err := Measure(value, limits); err != nil || nodes != 6 {
		t.Fatalf("nodes=%d err=%v", nodes, err)
	}
	for _, tt := range []struct {
		value  any
		limits Limits
		want   string
	}{
		{value, Limits{3, 5, 3}, "node count"},
		{value, Limits{2, 6, 3}, "depth"},
		{"long", limits, "string larger"},
		{map[string]any{"long": nil}, limits, "object key"},
		{1, limits, "unsupported type"},
	} {
		if _, err := Measure(tt.value, tt.limits); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("want %s: %v", tt.want, err)
		}
	}
}

func TestMeasureJSONPreservesNumberIdentityAndRejectsExtraValues(t *testing.T) {
	limits := Limits{Depth: MaxDepth, Nodes: MaxNodes, StringBytes: MaxStringBytes}
	if nodes, err := MeasureJSON([]byte(`{"n":9007199254740993,"a":[1,2]}`), limits); err != nil || nodes != 5 {
		t.Fatalf("nodes=%d err=%v", nodes, err)
	}
	for _, invalid := range []string{`{`, `1 2`, `[]null`, `1 invalid`} {
		if _, err := MeasureJSON([]byte(invalid), limits); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}
