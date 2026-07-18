package sqlresult

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuilderPreservesDuplicateColumns(t *testing.T) {
	builder := NewBuilder([]string{"id", "id", "id_2"}, 10, 4096, 1024, "...[truncated]")
	if !builder.Add([]any{1, 2, 3}, func(value any) any { return value }) {
		t.Fatal("expected row to be added")
	}
	result := builder.Result(nil)
	want := []string{"id", "id_2", "id_2_2"}
	for index := range want {
		if result.Columns[index] != want[index] {
			t.Fatalf("columns = %#v, want %#v", result.Columns, want)
		}
	}
	if len(result.Rows[0]) != 3 || result.Rows[0]["id"] != 1 || result.Rows[0]["id_2"] != 2 || result.Rows[0]["id_2_2"] != 3 {
		t.Fatalf("duplicate values were not preserved: %#v", result.Rows[0])
	}
}

func TestBoundValuePreservesUTF8WithinCellLimit(t *testing.T) {
	value, truncated := BoundValue("🙂🙂🙂", 8, "...[truncated]")
	text, ok := value.(string)
	if !ok || !truncated || len(text) > 8 || !utf8.ValidString(text) {
		t.Fatalf("bounded value = %#v, truncated=%v", value, truncated)
	}
}

func TestBuilderBoundsFinalSerializedOutput(t *testing.T) {
	const maxBytes = 640
	builder := NewBuilder([]string{"long_column_name", "other_column"}, 100, maxBytes, 200, "...[truncated]")
	for range 20 {
		if !builder.Add([]any{strings.Repeat("x", 160), strings.Repeat("y", 160)}, func(value any) any { return value }) {
			break
		}
	}
	result := builder.Result(map[string]any{"duration_ms": int64(123), "slow_query": false})
	encoded, err := json.Marshal(result.ToMap(maxBytes, map[string]any{"duration_ms": int64(123), "slow_query": false}))
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if len(encoded) > maxBytes {
		t.Fatalf("encoded result is %d bytes, want <= %d", len(encoded), maxBytes)
	}
	if !result.Truncated {
		t.Fatal("expected bounded result to report truncation")
	}
}
