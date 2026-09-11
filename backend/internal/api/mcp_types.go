package api

import (
	"time"
)

const (
	mcpInitialExecTimeout       = 45 * time.Second
	mcpBackgroundCommandTimeout = 30 * time.Minute
)

type mcpAuthContext struct {
	TokenID int64
	Name    string
	runtime databaseRuntime
}
