package api

import (
	"time"

	"github.com/aipermission/aipermission/backend/internal/commandrequests"
)

const (
	mcpInitialExecTimeout       = 45 * time.Second
	mcpBackgroundCommandTimeout = 30 * time.Minute
)

type mcpAuthContext struct {
	TokenID int64
	Name    string
	runtime *databaseRuntime
}

type commandPolicyWarning = commandrequests.PolicyWarning

const (
	commandRequestSourceMCP            = commandrequests.SourceMCP
	commandRequestSourceManual         = commandrequests.SourceManual
	runningCommandRequestAssistantHint = commandrequests.RunningAssistantHint
)

func analyzeCommandPolicy(command string) []commandPolicyWarning {
	return commandrequests.AnalyzePolicy(command)
}
