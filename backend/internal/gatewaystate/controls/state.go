// Package controls owns process-local authentication, UI session, maintenance,
// Vault request throttling, and backup operation controls.
package controls

import (
	"time"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/uisession"
)

const (
	AuthLockoutFailures      = 8
	MCPGlobalDelayFailures   = 32
	MCPGlobalLockoutFailures = 64
)

type State struct {
	MaintenanceConsole   console.MaintenanceConsoleRuntime
	AuthLimiter          *runtimecontrol.Auth
	MCPIPAuthLimiter     *runtimecontrol.Auth
	MCPTokenAuthLimiter  *runtimecontrol.Auth
	VaultRevealLimiter   *runtimecontrol.Window
	VaultGenerateLimiter *runtimecontrol.Window
	VaultRequestLimiter  *runtimecontrol.Window
	UISessions           *uisession.Manager
	BackupOperations     backups.OperationLimiter
}

func New(frontendPort string, maintenance console.MaintenanceConsoleRuntime) State {
	return State{
		MaintenanceConsole:   maintenance,
		AuthLimiter:          runtimecontrol.NewAuth(1, AuthLockoutFailures),
		MCPIPAuthLimiter:     runtimecontrol.NewAuth(MCPGlobalDelayFailures, MCPGlobalLockoutFailures),
		MCPTokenAuthLimiter:  runtimecontrol.NewAuth(1, AuthLockoutFailures),
		VaultRevealLimiter:   runtimecontrol.NewWindow(8, time.Minute),
		VaultGenerateLimiter: runtimecontrol.NewWindow(10, time.Minute),
		VaultRequestLimiter:  runtimecontrol.NewWindow(30, time.Minute),
		UISessions:           uisession.New(frontendPort),
	}
}
