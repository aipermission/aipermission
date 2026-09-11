package api

import (
	"time"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
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

type commandPolicyWarning = gatewayaccess.CommandPolicyWarning

const (
	commandRequestSourceMCP            = gatewayaccess.CommandSourceMCP
	commandRequestSourceManual         = gatewayaccess.CommandSourceManual
	runningCommandRequestAssistantHint = gatewayaccess.RunningAssistantHint
)

func analyzeCommandPolicy(command string) []commandPolicyWarning {
	return gatewayaccess.AnalyzeCommandPolicy(command)
}
