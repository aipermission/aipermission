package console

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

func PlainOutput(value string) string {
	if value == "" {
		return ""
	}
	value = ansiSequencePattern.ReplaceAllString(value, "")
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
