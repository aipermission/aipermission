package manualinput

import "github.com/aipermission/aipermission/backend/internal/console/manualinput/heredocsyntax"

type heredoc = heredocsyntax.Document

func heredocs(command string) []heredoc {
	return heredocsyntax.Parse(command, BufferLimit)
}
