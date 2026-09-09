package terminaltext

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

var ansiSequencePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\a]*(\a|\x1b\\)`)
var aptNoisePattern = regexp.MustCompile(`^\d+% \[|^Reading package lists\.\.\. \d+%$|^Building dependency tree\.\.\. \d+%$|^Reading state information\.\.\. \d+%$|^Scanning (processes|candidates|linux images)\.\.\. \[|^Scanning (processes|candidates|linux images)\.\.\.$`)
var shellPromptPattern = regexp.MustCompile(`^(?:[^@\s]+@[^:\s]+:.*|\[[^\]\r\n]{1,128}\]\s*|(?:~|/)[^#$\r\n]{0,128}\s*)[#$]\s*.*$`)
var bareShellPromptPattern = regexp.MustCompile(`^(?:[^@\s]+@[^:\s]+:.*|\[[^\]\r\n]{1,128}\]\s*|(?:~|/)[^#$\r\n]{0,128}\s*)[#$]\s*$`)

func StripANSI(value string) string {
	return ansiSequencePattern.ReplaceAllString(value, "")
}

func IsShellPrompt(value string) bool {
	return shellPromptPattern.MatchString(value)
}

func IsBareShellPrompt(value string) bool {
	return bareShellPromptPattern.MatchString(value)
}

func PlainOutput(value string) string {
	if value == "" {
		return ""
	}
	value = StripANSI(value)
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "__AIPERMISSION_EXIT_") || strings.Contains(trimmed, "stty -echo") || strings.Contains(trimmed, "stty sane") {
			continue
		}
		if isPlainOutputNoise(trimmed) {
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func isPlainOutputNoise(line string) bool {
	if aptNoisePattern.MatchString(line) {
		return true
	}
	if strings.Contains(line, "[Working]") || strings.Contains(line, "Waiting for headers") || strings.Contains(line, "Connected to mirror.") || strings.Contains(line, "Packages store") {
		return true
	}
	if line == ">" {
		return true
	}
	if strings.Contains(line, "__aipermission_saved_") || strings.Contains(line, "stty -echo") || strings.Contains(line, "stty echo icanon opost") || line == "PS2=" {
		return true
	}
	if shellPromptPattern.MatchString(line) {
		return true
	}
	if strings.HasPrefix(line, "(") && strings.Contains(line, "database ...") {
		return true
	}
	if strings.HasPrefix(line, "root@") && strings.HasSuffix(line, "#") {
		return true
	}
	exactNoise := []string{
		"Scanning processes...",
		"Scanning candidates...",
		"Scanning linux images...",
	}
	return slices.Contains(exactNoise, line)
}

func TailStringByBytes(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	start := len(value) - maxBytes
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}

func FormatAutomationCommand(command string) string {
	lines := strings.Split(strings.TrimRight(command, "\r\n"), "\n")
	var builder strings.Builder
	builder.WriteString("[AI command]\r\n")
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		builder.WriteString("$ ")
		builder.WriteString(line)
		builder.WriteString("\r\n")
	}
	return builder.String()
}

func CleanDisplayOutput(data string, keepShellPrompt bool) string {
	if data == "" {
		return ""
	}
	data = StripANSI(data)
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")
	lines := strings.Split(data, "\n")
	endedWithNewline := strings.HasSuffix(data, "\n")
	if !endedWithNewline && len(lines) > 0 {
		last := lines[len(lines)-1]
		if isDisplayNoise(strings.TrimSpace(last), keepShellPrompt) {
			lines = lines[:len(lines)-1]
		}
	}

	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		trimmed := strings.TrimSpace(line)
		if isDisplayNoise(trimmed, keepShellPrompt) || trimmed == "" {
			continue
		}
		cleaned = append(cleaned, line)
	}
	if len(cleaned) == 0 {
		return ""
	}
	output := strings.Join(cleaned, "\r\n")
	if endedWithNewline {
		output += "\r\n"
	}
	return output
}

func CleanCommandResultOutput(data string) string {
	if data == "" {
		return ""
	}
	data = StripANSI(data)
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")
	lines := strings.Split(data, "\n")
	endedWithNewline := strings.HasSuffix(data, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if isInternalNoise(strings.TrimSpace(line)) {
			continue
		}
		cleaned = append(cleaned, line)
	}
	if len(cleaned) == 0 {
		return ""
	}
	output := strings.Join(cleaned, "\n")
	if endedWithNewline && !strings.HasSuffix(output, "\n") {
		output += "\n"
	}
	return output
}

func isDisplayNoise(line string, keepShellPrompt bool) bool {
	if line == "" {
		return false
	}
	if isInternalNoise(line) {
		return true
	}
	if shellPromptPattern.MatchString(line) {
		return !keepShellPrompt || !bareShellPromptPattern.MatchString(line)
	}
	return false
}

func isInternalNoise(line string) bool {
	if line == "" {
		return false
	}
	return strings.Contains(line, "__AIPERMISSION_EXIT_") ||
		strings.Contains(line, "__aipermission_saved_stty") ||
		strings.Contains(line, "__aipermission_saved_ps2") ||
		strings.Contains(line, "stty -echo") ||
		strings.Contains(line, "stty sane") ||
		strings.Contains(line, "stty echo icanon opost") ||
		strings.Contains(line, "stty echo") ||
		line == "PS2=" ||
		line == ">"
}
