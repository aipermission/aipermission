package terminaltext

import (
	"fmt"
	"strconv"
	"strings"
)

// CommandExitMarker distinguishes a complete exit marker from a partial stream
// frame. A marker at the start is valid only when the preceding transcript was truncated.
func CommandExitMarker(segment, marker string, truncated bool) (output string, exitCode int, found, complete bool, err error) {
	needle := "\n" + marker + ":"
	index := strings.Index(segment, needle)
	length := len(needle)
	if index < 0 && truncated && strings.HasPrefix(segment, marker+":") {
		index, length = 0, len(marker)+1
	}
	if index < 0 {
		return segment, 1, false, false, nil
	}
	after := segment[index+length:]
	lineEnd := strings.IndexAny(after, "\r\n")
	if lineEnd < 0 {
		return segment, 1, true, false, nil
	}
	output = CleanCommandResultOutput(segment[:index])
	text := after[:lineEnd]
	if text == "" || strings.IndexFunc(text, func(value rune) bool { return value < '0' || value > '9' }) >= 0 {
		return output, 1, true, false, fmt.Errorf("invalid console command exit marker")
	}
	exitCode, err = strconv.Atoi(text)
	if err != nil {
		return output, 1, true, false, fmt.Errorf("parse console command exit status: %w", err)
	}
	return output, exitCode, true, true, nil
}
