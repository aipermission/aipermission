package terminaltext

import "strings"

func ManualSegmentHasPrompt(segment string) bool {
	plain := normalizeTerminalText(segment)
	plain = strings.TrimRight(plain, " \t\n")
	if plain == "" {
		return false
	}
	lines := strings.Split(plain, "\n")
	return IsBareShellPrompt(strings.TrimRight(lines[len(lines)-1], " \t"))
}

func LastManualShellPrompt(transcript string) string {
	plain := strings.TrimRight(normalizeTerminalText(transcript), " \t\n")
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

func ManualTranscriptEndsWithPrompt(transcript string, prompt string) bool {
	prompt = strings.TrimRight(normalizeTerminalText(prompt), " \t\n")
	plain := strings.TrimRight(normalizeTerminalText(transcript), " \t\n")
	if plain == "" {
		return false
	}
	lines := strings.Split(plain, "\n")
	last := strings.TrimRight(lines[len(lines)-1], " \t")
	if !IsBareShellPrompt(last) {
		return false
	}
	return prompt == "" || last == prompt
}

func ManualCapturedOutput(segment string, command string, maxBytes int) (string, bool) {
	lines := strings.Split(normalizeTerminalText(segment), "\n")
	commandLines := manualCommandLines(command)
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		if !lineContainsAnyCommandEcho(line, commandLines) {
			cleaned = append(cleaned, line)
		}
	}
	lines = cleaned
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 0 && IsBareShellPrompt(strings.TrimRight(lines[len(lines)-1], " \t")) {
		lines = lines[:len(lines)-1]
	}
	output := strings.Join(lines, "\n")
	if maxBytes > 0 && len(output) > maxBytes {
		return TailStringByBytes(output, maxBytes), true
	}
	return output, false
}

func normalizeTerminalText(value string) string {
	value = StripANSI(value)
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func manualPromptPrefix(line string) string {
	line = strings.TrimRight(normalizeTerminalText(line), " \t\n")
	if line == "" {
		return ""
	}
	if IsBareShellPrompt(line) {
		return line
	}
	if !IsShellPrompt(line) {
		return ""
	}
	index := strings.LastIndex(line, "# ")
	if dollarIndex := strings.LastIndex(line, "$ "); dollarIndex > index {
		index = dollarIndex
	}
	if index < 0 {
		return ""
	}
	return strings.TrimRight(line[:index+1], " \t")
}

func manualCommandLines(command string) []string {
	lines := strings.Split(strings.ReplaceAll(command, "\r\n", "\n"), "\n")
	values := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
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
	if IsBareShellPrompt(line) || !IsShellPrompt(line) {
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
