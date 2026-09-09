package console

import "github.com/aipermission/aipermission/backend/internal/console/terminaltext"

// PlainOutput returns bounded-display-friendly command output without terminal
// control sequences or AIPermission's internal console protocol noise.
func PlainOutput(value string) string {
	return terminaltext.PlainOutput(value)
}

// TailStringByBytes keeps the UTF-8-safe tail used by console consumers.
func TailStringByBytes(value string, maxBytes int) string {
	return terminaltext.TailStringByBytes(value, maxBytes)
}
