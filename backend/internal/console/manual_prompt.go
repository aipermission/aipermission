package console

import "strings"

func (s *managedConsoleSession) clearManualPauseIfPromptReturnedLocked() {
	if s == nil || s.manualPause == nil {
		return
	}
	startOffset := s.manualPause.StartOffset
	if startOffset < 0 || startOffset > len(s.rawTranscript) {
		startOffset = 0
	}
	if startOffset < len(s.rawTranscript) && manualTranscriptEndsWithPrompt(s.rawTranscript[startOffset:], s.manualPause.Prompt) {
		s.manualPause = nil
		s.manualInput.reset()
	}
}

func manualReasonPausesCapture(reason string) bool {
	switch reason {
	case "interactive_editor", "interactive_repl", "interactive_tui", "nested_shell", "long_running_stream", "may_prompt":
		return true
	default:
		return false
	}
}

func manualActiveIsHistoryRecall(active *consoleSessionManualCapture) bool {
	if active == nil {
		return false
	}
	return active.CompletionTrackingReason == "history_recall_untracked" || active.Command == "command recalled with arrow key"
}

func manualSegmentHasPrompt(segment string) bool {
	plain := ansiSequencePattern.ReplaceAllString(segment, "")
	plain = strings.ReplaceAll(plain, "\r\n", "\n")
	plain = strings.ReplaceAll(plain, "\r", "\n")
	plain = strings.TrimRight(plain, " \t\n")
	if plain == "" {
		return false
	}
	lines := strings.Split(plain, "\n")
	last := strings.TrimRight(lines[len(lines)-1], " \t")
	return bareShellPromptPattern.MatchString(last)
}

func lastManualShellPrompt(transcript string) string {
	plain := normalizedPlainTerminalText(transcript)
	plain = strings.TrimRight(plain, " \t\n")
	if plain == "" {
		return ""
	}
	lines := strings.Split(plain, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if prompt := manualPromptPrefix(lines[index]); prompt != "" {
			return prompt
		}
	}
	return ""
}

func manualTranscriptEndsWithPrompt(transcript string, prompt string) bool {
	prompt = strings.TrimRight(normalizedPlainTerminalText(prompt), " \t\n")
	plain := strings.TrimRight(normalizedPlainTerminalText(transcript), " \t\n")
	if plain == "" {
		return false
	}
	lines := strings.Split(plain, "\n")
	last := strings.TrimRight(lines[len(lines)-1], " \t")
	if !bareShellPromptPattern.MatchString(last) {
		return false
	}
	return prompt == "" || last == prompt
}

func manualPromptPrefix(line string) string {
	line = strings.TrimRight(normalizedPlainTerminalText(line), " \t\n")
	if line == "" {
		return ""
	}
	if bareShellPromptPattern.MatchString(line) {
		return line
	}
	if !shellPromptPattern.MatchString(line) {
		return ""
	}
	hashIndex := strings.LastIndex(line, "# ")
	dollarIndex := strings.LastIndex(line, "$ ")
	index := hashIndex
	if dollarIndex > index {
		index = dollarIndex
	}
	if index < 0 {
		return ""
	}
	return strings.TrimRight(line[:index+1], " \t")
}

func manualCapturedOutput(segment string, command string) (string, bool) {
	plain := normalizedPlainTerminalText(segment)
	lines := strings.Split(plain, "\n")
	commandLines := manualCommandLines(command)
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		if lineContainsAnyCommandEcho(line, commandLines) {
			continue
		}
		cleaned = append(cleaned, line)
	}
	lines = cleaned
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 0 && bareShellPromptPattern.MatchString(strings.TrimRight(lines[len(lines)-1], " \t")) {
		lines = lines[:len(lines)-1]
	}
	output := strings.Join(lines, "\n")
	truncated := false
	if len(output) > maxManualCapturedOutputBytes {
		output = TailStringByBytes(output, maxManualCapturedOutputBytes)
		truncated = true
	}
	return output, truncated
}

func normalizedPlainTerminalText(value string) string {
	value = ansiSequencePattern.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return value
}

func manualCommandLines(command string) []string {
	lines := strings.Split(strings.ReplaceAll(command, "\r\n", "\n"), "\n")
	values := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			values = append(values, line)
		}
	}
	return values
}

func lineContainsAnyCommandEcho(line string, commands []string) bool {
	for _, command := range commands {
		if lineContainsCommandEcho(line, command) {
			return true
		}
	}
	return false
}

func lineContainsCommandEcho(line string, command string) bool {
	line = strings.TrimSpace(line)
	command = strings.TrimSpace(command)
	if line == command {
		return true
	}
	if bareShellPromptPattern.MatchString(line) || !shellPromptPattern.MatchString(line) {
		return false
	}
	if index := strings.LastIndex(line, "# "); index >= 0 {
		return strings.TrimSpace(line[index+2:]) == command
	}
	if index := strings.LastIndex(line, "$ "); index >= 0 {
		return strings.TrimSpace(line[index+2:]) == command
	}
	return false
}
