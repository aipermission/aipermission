package transport

import (
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

type LiveConsoleOptions struct {
	ForceShellCommand        string
	StartupInputAfterConnect string
	Generation               int64
	HasEnvironment           bool
	Environment              *sessionenv.Envelope
}
