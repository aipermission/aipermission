package rolejournal

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseEntryPreservesCompleteSnapshotAndExactResourceID(t *testing.T) {
	entry := boundTest(t, New(newMemoryStore()))
	for _, id := range []int64{1, 9007199254740993, 9223372036854775807} {
		entry.ResourceID = id
		encoded, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(encoded, &value); err != nil {
			t.Fatal(err)
		}
		for _, input := range []any{entry, value} {
			got, err := ParseEntry(input)
			if err != nil || got != entry {
				t.Fatalf("snapshot lost exact identity: got=%#v want=%#v err=%v", got, entry, err)
			}
		}
	}
}

func TestParseEntryRejectsInvalidShapeIdentityAndRecord(t *testing.T) {
	entry := boundTest(t, New(newMemoryStore()))
	for name, input := range map[string]any{
		"nil": nil, "array": []any{entry}, "string": "{}", "unserializable": make(chan int),
		"empty": map[string]any{}, "missing record": map[string]any{"resource_id": "1"},
		"missing ID":     map[string]any{"record": entry.Record},
		"unknown field":  map[string]any{"resource_id": "1", "record": entry.Record, "extra": true},
		"numeric ID":     map[string]any{"resource_id": 1, "record": entry.Record},
		"invalid record": map[string]any{"resource_id": "1", "record": Record{}},
		"oversized":      map[string]any{"resource_id": "1", "record": entry.Record, "extra": strings.Repeat("x", maxRecordBytes)},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := ParseEntry(input); err == nil || got != (Entry{}) {
				t.Fatalf("invalid snapshot accepted: %#v %v", got, err)
			}
		})
	}
	for _, id := range []string{"", "0", "-1", "01", "+1", " 1", "1 ", "1.0", "1e0", "9223372036854775808"} {
		t.Run("id/"+id, func(t *testing.T) {
			if got, err := ParseEntry(map[string]any{"resource_id": id, "record": entry.Record}); err == nil || got != (Entry{}) {
				t.Fatalf("invalid resource ID accepted: %#v %v", got, err)
			}
		})
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	for _, altered := range []string{
		strings.Replace(string(encoded), `"version":1`, `"version":1,"extra":true`, 1),
		strings.Replace(string(encoded), `"anchor":{`, `"anchor":{"extra":true,`, 1),
	} {
		if got, err := ParseEntry(json.RawMessage(altered)); err == nil || got != (Entry{}) {
			t.Fatalf("unknown nested snapshot field accepted: %#v %v", got, err)
		}
	}
}
