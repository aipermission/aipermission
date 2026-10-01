package connectors

import "strings"

// QuoteShellArgument encodes one POSIX argument, not an executable or a complete
// command. Callers still validate identities and reject protocol-invalid values.
func QuoteShellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

const InteractiveShellProbe = "if command -v bash >/dev/null 2>&1; then exec bash -l; fi; exec sh"
