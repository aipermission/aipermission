package manualinput

import (
	"reflect"
	"strings"
	"testing"
)

func TestCollapsePreservesSingleObservation(t *testing.T) {
	for _, records := range [][]Record{nil, {}, {{Command: "pwd", TrackingReason: "untrusted_command_text"}}} {
		if got := Collapse(records); !reflect.DeepEqual(got, records) {
			t.Fatalf("single observation changed: %#v; want %#v", got, records)
		}
	}
}

func TestCollapseSynthesizesBoundedCompoundObservation(t *testing.T) {
	cases := []struct {
		name    string
		records []Record
		want    Record
	}{
		{"trackable", []Record{{Command: "pwd", TrackOutput: true}, {Command: "hostname", TrackOutput: true}}, Record{Command: "pwd\nhostname", TrackingReason: "manual_output_not_tracked", TrackOutput: true}},
		{"unsafe", []Record{{Command: "pwd", TrackOutput: true}, {Command: "vim", TrackingReason: "interactive_editor"}}, Record{Command: "pwd\nvim", TrackingReason: "compound_command"}},
		{"recalled", []Record{{Command: "command recalled with arrow key", TrackOutput: true, CompletionTrackingReason: "history_recall_untracked"}, {Command: "pwd", TrackOutput: true}}, Record{Command: "command recalled with arrow key\npwd", TrackingReason: "manual_output_not_tracked", TrackOutput: true, CompletionTrackingReason: "history_recall_untracked"}},
		{"bounded", []Record{{Command: strings.Repeat("x", PreviewLimit), TrackOutput: true}, {Command: "extra", TrackOutput: true}}, Record{Command: strings.Repeat("x", PreviewLimit-4) + " ...", TrackingReason: "command_preview_truncated"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]Record(nil), tc.records...)
			if got := Collapse(tc.records); !reflect.DeepEqual(got, []Record{tc.want}) {
				t.Fatalf("compound = %#v; want %#v", got, tc.want)
			}
			if !reflect.DeepEqual(tc.records, before) {
				t.Fatalf("caller observations changed: %#v; want %#v", tc.records, before)
			}
		})
	}
}
