package transport

import (
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type LiveConsoleOptions struct {
	ForceShellCommand        string
	StartupInputAfterConnect string
	Generation               int64
	HasEnvironment           bool
	Environment              connectorapi.SessionEnvironment
}
