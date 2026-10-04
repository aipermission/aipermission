package manualinput

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestManualInputTextContract(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   []Record
	}{
		{"empty", []string{"", "\n"}, nil},
		{"split line and CRLF", []string{"echo ", "ok\r", "\n"}, []Record{{Command: "echo ok", TrackingReason: "manual_output_not_tracked", TrackOutput: true}}},
		{"unicode backspace", []string{"echo ş界\b\x7fok\n"}, []Record{{Command: "echo ok", TrackingReason: "manual_output_not_tracked", TrackOutput: true}}},
		{"tab", []string{"echo\tok\n"}, []Record{{Command: "echo\tok", TrackingReason: "manual_output_not_tracked", TrackOutput: true}}},
		{"paste", []string{"\x1b[200~echo ok\x1b[201~\n"}, []Record{{Command: "echo ok", TrackingReason: "manual_output_not_tracked", TrackOutput: true}}},
		{"split arrow recall", []string{"\x1b", "[", "A", "\r"}, []Record{{Command: "command recalled with arrow key", TrackingReason: "history_recall_untracked", TrackOutput: true, CompletionTrackingReason: "history_recall_untracked"}}},
		{"down arrow recall", []string{"\x1b[B\n"}, []Record{{Command: "command recalled with arrow key", TrackingReason: "history_recall_untracked", TrackOutput: true, CompletionTrackingReason: "history_recall_untracked"}}},
		{"escape edited text", []string{"echo \x1b[Dchanged\n"}, []Record{{Command: "echo changed", TrackingReason: "untrusted_command_text"}}},
		{"OSC", []string{"\x1b]title", "\aecho ok\n"}, []Record{{Command: "echo ok", TrackingReason: "untrusted_command_text"}}},
		{"non CSI escape", []string{"\x1bxecho ok\n"}, []Record{{Command: "echo ok", TrackingReason: "untrusted_command_text"}}},
		{"control text", []string{"echo\x01 ok\n"}, []Record{{Command: "echo ok", TrackingReason: "untrusted_command_text"}}},
		{"cancel", []string{"discard\x03echo ok\n"}, []Record{{Command: "echo ok", TrackingReason: "manual_output_not_tracked", TrackOutput: true}}},
		{"end input", []string{"discard\x04echo ok\n"}, []Record{{Command: "echo ok", TrackingReason: "manual_output_not_tracked", TrackOutput: true}}},
		{"heredoc", []string{"cat <<'EOF'\n", "hidden\nE", "OF\necho ok\n"}, []Record{{Command: "cat <<'EOF' ...", TrackingReason: "multiline_or_heredoc"}, {Command: "echo ok", TrackingReason: "manual_output_not_tracked", TrackOutput: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var capture Capture
			var got []Record
			for _, chunk := range tc.chunks {
				got = append(got, capture.Consume(chunk)...)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("records = %#v; want %#v", got, tc.want)
			}
		})
	}
}

func TestManualInputBoundedPreviewAndResetContract(t *testing.T) {
	for _, text := range []string{strings.Repeat("x", BufferLimit+1), strings.Repeat("界", BufferLimit/3+1)} {
		var capture Capture
		capture.Consume(text)
		capture.Consume("discarded")
		got := capture.Consume("\n")
		if len(got) != 1 || got[0].TrackOutput || got[0].TrackingReason != "command_preview_truncated" || len(got[0].Command) > PreviewLimit || !utf8.ValidString(got[0].Command) {
			t.Fatalf("unbounded or invalid preview: %#v", got)
		}
		capture.Consume("cat <<EOF\nsecret\n")
		capture.Reset()
		got = capture.Consume("hostname\n")
		if len(got) != 1 || got[0].Command != "hostname" || !got[0].TrackOutput {
			t.Fatalf("Reset retained heredoc or truncation state: %#v", got)
		}
	}
}

func TestManualCommandClassificationContract(t *testing.T) {
	cases := map[string]string{
		"": "manual_output_not_tracked", "hostname": "manual_output_not_tracked", "/usr/bin/vim file": "interactive_editor",
		"python": "interactive_repl", "python -i script.py": "interactive_repl", "python script.py": "manual_output_not_tracked",
		"htop": "interactive_tui", "tmux attach": "nested_shell", "tail -f file": "long_running_stream", "tail --follow file": "long_running_stream", "tail file": "manual_output_not_tracked",
		"docker exec -it id sh": "nested_shell", "kubectl exec pod --stdin": "nested_shell", "docker ps": "manual_output_not_tracked",
		"sudo hostname": "may_prompt", "sleep 1 &": "background_job", "cat << EOF": "multiline_or_heredoc",
	}
	for command, want := range cases {
		t.Run(command, func(t *testing.T) {
			got := classifyManualCommand(command, false)
			if got.TrackingReason != want || got.TrackOutput != (want == "manual_output_not_tracked") {
				t.Fatalf("classification = %#v; want %s", got, want)
			}
		})
	}
	if got := classifyManualCommand("partial", true); got.Command != "partial ..." || got.TrackOutput {
		t.Fatalf("truncation = %#v", got)
	}
	for command, want := range map[string]string{"no heredoc": "", "cat <<": "", "cat <<-": "", "cat << ''": "", "cat << path/name": "", "cat << path\\name": "", "cat <<-\"EOF\"": "EOF"} {
		if got := heredocTerminator(command); got != want {
			t.Fatalf("terminator(%q) = %q; want %q", command, got, want)
		}
	}
	for command, want := range map[string]string{"partial...": "partial...", "partial  ": "partial ..."} {
		if got := Preview(command, true); got != want {
			t.Fatalf("preview(%q) = %q; want %q", command, got, want)
		}
	}
	if got := Preview(strings.Repeat("界", PreviewLimit), false); len(got) > PreviewLimit+4 || !utf8.ValidString(got) {
		t.Fatalf("invalid bounded unicode preview: %q", got)
	}
	if trimLastRune("") != "" || trimLastRune("a界") != "a" {
		t.Fatal("backspace must remove exactly one rune")
	}
}

func TestInteractiveReasonsPauseCapture(t *testing.T) {
	for _, reason := range []string{"interactive_editor", "interactive_repl", "interactive_tui", "nested_shell", "long_running_stream", "may_prompt"} {
		if !PausesCapture(reason) {
			t.Fatalf("interactive reason %q did not pause capture", reason)
		}
	}
	for _, reason := range []string{"manual_output_not_tracked", "untrusted_command_text", "background_job", "multiline_or_heredoc", "history_recall_untracked", "command_preview_truncated", ""} {
		if PausesCapture(reason) {
			t.Fatalf("noninteractive reason %q paused capture", reason)
		}
	}
}
