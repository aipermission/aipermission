package sqlexecute

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCollectorBoundsEscapedMetadataAndCells(t *testing.T) {
	collector := NewCollector(1000)
	for i := 0; i < 200; i++ {
		collector.Command(strings.Repeat("\x00", 2048), 9223372036854775807)
		collector.Notice(strings.Repeat("\x00", 2048))
		if err := collector.BeginResult([]string{strings.Repeat("a", 512)}); err != nil {
			t.Fatal(err)
		}
		if err := collector.AddRow([]any{strings.Repeat("\x00", MaxCellBytes*2)}); err != nil {
			t.Fatal(err)
		}
		collector.EndResult()
	}
	output := collector.Output(1)
	encoded, err := json.Marshal(output)
	if err != nil || len(encoded) > MaxOutputBytes || output["truncated"] != true || len(collector.results) > 32 || len(collector.commands) > 128 || len(collector.notices) > 32 {
		t.Fatalf("unbounded execution output: bytes=%d err=%v", len(encoded), err)
	}
	if len(collector.commands) == 0 || collector.commands[0].ReportedRows != "9223372036854775807" {
		t.Fatal("large reported count lost precision")
	}
}

func TestCollectorRejectsInvalidResultState(t *testing.T) {
	collector := NewCollector(1)
	if err := collector.BeginResult(make([]string, 257)); err != nil || collector.RetainingRows() {
		t.Fatal("wide results must be drained without retaining their columns")
	}
	collector.EndResult()
	if err := collector.AddRow(nil); err == nil {
		t.Fatal("row outside a result accepted")
	}
	if err := collector.BeginResult(nil); err != nil {
		t.Fatal(err)
	}
	if err := collector.BeginResult(nil); err == nil {
		t.Fatal("nested empty result accepted")
	}
	collector.EndResult()
	if err := collector.BeginResult([]string{"duplicate", "duplicate"}); err != nil {
		t.Fatal(err)
	}
	if err := collector.AddRow([]any{1}); err == nil {
		t.Fatal("malformed row accepted")
	}
	if err := collector.AddRow([]any{1, 2}); err != nil {
		t.Fatal(err)
	}
	collector.EndResult()
	if got := collector.results[1].Columns; got[0] != "duplicate" || got[1] != "duplicate_2" {
		t.Fatalf("duplicate columns lost: %v", got)
	}
}
