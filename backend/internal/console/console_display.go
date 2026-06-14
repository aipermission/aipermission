package console

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func scanConsoleSession(scanner interface {
	Scan(dest ...any) error
}) (Record, error) {
	var item Record
	var closedAt sql.NullString
	if err := scanner.Scan(
		&item.ID,
		&item.RuntimeID,
		&item.TargetName,
		&item.Name,
		&item.Status,
		&item.Transcript,
		&item.Error,
		&item.Cols,
		&item.Rows,
		&item.CreatedAt,
		&item.UpdatedAt,
		&closedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Record{}, err
		}
		return Record{}, fmt.Errorf("scan console session: %w", err)
	}
	if closedAt.Valid {
		item.ClosedAt = &closedAt.String
	}
	return item, nil
}

func limitConsoleTranscript(value string) string {
	if len(value) <= maxConsoleTranscriptLength {
		return value
	}
	return TailStringByBytes(value, maxConsoleTranscriptLength)
}

func formatAutomationCommand(command string) string {
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

func cleanConsoleDisplayOutput(data string, keepShellPrompt bool) string {
	if data == "" {
		return ""
	}
	data = ansiSequencePattern.ReplaceAllString(data, "")
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")
	lines := strings.Split(data, "\n")
	endedWithNewline := strings.HasSuffix(data, "\n")
	if !endedWithNewline && len(lines) > 0 {
		last := lines[len(lines)-1]
		if isConsoleDisplayNoise(strings.TrimSpace(last), keepShellPrompt) {
			lines = lines[:len(lines)-1]
		}
	}

	cleaned := make([]string, 0, len(lines))
	for index, line := range lines {
		if index == len(lines)-1 && !endedWithNewline {
			// Keep partial non-noise chunks so long-running commands still feel live.
		}
		line = strings.TrimRight(line, " \t")
		trimmed := strings.TrimSpace(line)
		if isConsoleDisplayNoise(trimmed, keepShellPrompt) {
			continue
		}
		if trimmed == "" {
			continue
		}
		cleaned = append(cleaned, line)
	}
	if len(cleaned) == 0 {
		return ""
	}
	output := strings.Join(cleaned, "\r\n")
	if endedWithNewline && output != "" {
		output += "\r\n"
	}
	return output
}

func cleanConsoleCommandResultOutput(data string) string {
	if data == "" {
		return ""
	}
	data = ansiSequencePattern.ReplaceAllString(data, "")
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")
	lines := strings.Split(data, "\n")
	endedWithNewline := strings.HasSuffix(data, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if isConsoleInternalNoise(strings.TrimSpace(line)) {
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

func isConsoleDisplayNoise(line string, keepShellPrompt bool) bool {
	if line == "" {
		return false
	}
	if isConsoleInternalNoise(line) {
		return true
	}
	if shellPromptPattern.MatchString(line) {
		if keepShellPrompt && bareShellPromptPattern.MatchString(line) {
			return false
		}
		return true
	}
	return false
}

func isConsoleInternalNoise(line string) bool {
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
