package api

import (
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	"time"
)

const (
	mcpInitialExecTimeout       = 45 * time.Second
	mcpBackgroundCommandTimeout = 30 * time.Minute
)

type mcpAuthContext struct {
	TokenID int64
	Name    string
	runtime *gatewayinfra.WorkspaceHandle
}
