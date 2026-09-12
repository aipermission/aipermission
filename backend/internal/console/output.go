package console

import "github.com/aipermission/aipermission/backend/internal/console/terminaltext"

func PlainOutput(value string) string { return terminaltext.PlainOutput(value) }
func TailStringByBytes(value string, maxBytes int) string {
	return terminaltext.TailStringByBytes(value, maxBytes)
}
