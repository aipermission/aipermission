package console

import (
	"strings"
	"unicode/utf8"
)

type manualCommandRecord struct {
	Command                  string
	TrackingReason           string
	TrackOutput              bool
	StartOffset              int
	ResumePrompt             string
	CompletionTrackingReason string
}

func collapseManualCommandRecords(commands []manualCommandRecord) []manualCommandRecord {
	if len(commands) <= 1 {
		return commands
	}
	trackOutput := true
	startOffset := commands[0].StartOffset
	parts := make([]string, 0, len(commands))
	for _, command := range commands {
		parts = append(parts, command.Command)
		if !command.TrackOutput {
			trackOutput = false
		}
	}
	joined := strings.Join(parts, "\n")
	reason := "manual_output_not_tracked"
	if !trackOutput {
		reason = "compound_command"
	}
	if len(joined) > maxManualCommandPreviewBytes {
		reason = "command_preview_truncated"
		trackOutput = false
	}
	return []manualCommandRecord{{
		Command:                  manualCommandPreview(joined, reason == "command_preview_truncated"),
		TrackingReason:           reason,
		TrackOutput:              trackOutput,
		StartOffset:              startOffset,
		ResumePrompt:             commands[0].ResumePrompt,
		CompletionTrackingReason: commands[0].CompletionTrackingReason,
	}}
}

func classifyManualCommand(command string, truncated bool) manualCommandRecord {
	reason := "manual_output_not_tracked"
	if truncated || len(command) > maxManualCommandPreviewBytes {
		reason = "command_preview_truncated"
	}
	if heredocTerminator(command) != "" {
		reason = "multiline_or_heredoc"
	}
	if reason == "manual_output_not_tracked" {
		reason = classifyManualCommandReason(command)
	}
	return manualCommandRecord{
		Command:        manualCommandPreview(command, reason == "command_preview_truncated" || reason == "multiline_or_heredoc"),
		TrackingReason: reason,
		TrackOutput:    reason == "manual_output_not_tracked",
	}
}

func classifyManualCommandReason(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "manual_output_not_tracked"
	}
	base := fields[0]
	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}
	switch base {
	case "nano", "vi", "vim", "nvim", "emacs":
		return "interactive_editor"
	case "psql", "mysql", "redis-cli", "sqlite3", "python", "python3", "node", "irb", "rails", "php":
		if len(fields) == 1 || strings.HasPrefix(command, base+" -i") {
			return "interactive_repl"
		}
	case "top", "htop", "btop", "less", "more", "man", "watch":
		return "interactive_tui"
	case "ftp", "telnet", "mosh", "tmux", "screen":
		return "nested_shell"
	case "tail":
		if containsField(fields[1:], "-f") || containsField(fields[1:], "--follow") {
			return "long_running_stream"
		}
	case "docker", "kubectl":
		if commandContainsInteractiveFlag(fields[1:]) {
			return "nested_shell"
		}
	case "sudo":
		return "may_prompt"
	}
	if strings.HasSuffix(strings.TrimSpace(command), "&") {
		return "background_job"
	}
	return "manual_output_not_tracked"
}

func containsField(fields []string, value string) bool {
	for _, field := range fields {
		if field == value {
			return true
		}
	}
	return false
}

func commandContainsInteractiveFlag(fields []string) bool {
	for _, field := range fields {
		if field == "-it" || field == "-ti" || field == "-i" || field == "-t" || field == "--tty" || field == "--stdin" {
			return true
		}
	}
	return false
}

func manualCommandPreview(command string, incomplete bool) string {
	command = strings.TrimSpace(command)
	limit := maxManualCommandPreviewBytes
	if incomplete && limit > 4 {
		limit -= 4
	}
	if len(command) > limit {
		command = command[:limit]
		for len(command) > 0 && !utf8.ValidString(command) {
			command = command[:len(command)-1]
		}
		incomplete = true
	}
	if incomplete && !strings.HasSuffix(command, "...") {
		command = strings.TrimRight(command, " \t") + " ..."
	}
	return command
}

func heredocTerminator(command string) string {
	index := strings.Index(command, "<<")
	if index < 0 {
		return ""
	}
	rest := strings.TrimSpace(command[index+2:])
	if strings.HasPrefix(rest, "-") {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "-"))
	}
	if rest == "" {
		return ""
	}
	token := strings.Fields(rest)[0]
	token = strings.Trim(token, `"'`)
	if token == "" || strings.ContainsAny(token, `/\`) {
		return ""
	}
	return token
}

func trimLastRune(value string) string {
	if value == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(value)
	if size <= 0 {
		return ""
	}
	return value[:len(value)-size]
}
