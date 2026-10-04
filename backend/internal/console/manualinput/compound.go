package manualinput

import "strings"

// Collapse treats one submitted batch as a single observation. It does not
// assign stream positions, infer execution or merge independent submissions.
func Collapse(records []Record) []Record {
	if len(records) <= 1 {
		return records
	}
	trackOutput := true
	parts := make([]string, 0, len(records))
	for _, record := range records {
		parts = append(parts, record.Command)
		if !record.TrackOutput {
			trackOutput = false
		}
	}
	joined := strings.Join(parts, "\n")
	reason := "manual_output_not_tracked"
	if !trackOutput {
		reason = "compound_command"
	}
	if len(joined) > PreviewLimit {
		reason = "command_preview_truncated"
		trackOutput = false
	}
	return []Record{{
		Command:                  Preview(joined, reason == "command_preview_truncated"),
		TrackingReason:           reason,
		TrackOutput:              trackOutput,
		CompletionTrackingReason: records[0].CompletionTrackingReason,
	}}
}
