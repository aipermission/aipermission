package connectors

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

var commandExecutablePath = regexp.MustCompile(`^/[A-Za-z0-9_./+-]+$`)
var commandExecutableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// NormalizeCommandExecutable accepts one connector-declared binary name or an
// absolute POSIX wrapper path, never arguments or parent-directory traversal.
func NormalizeCommandExecutable(value, standard string) (string, error) {
	if !commandExecutableName.MatchString(standard) {
		return "", fmt.Errorf("invalid standard executable name")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = standard
	}
	if value == standard {
		return value, nil
	}
	if len(value) > 1024 || !path.IsAbs(value) || !commandExecutablePath.MatchString(value) || strings.Contains(value, "/../") || strings.HasSuffix(value, "/..") {
		return "", fmt.Errorf("executable must be the standard binary or an absolute wrapper path without arguments or parent traversal")
	}
	return value, nil
}
