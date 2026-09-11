// Package workspaces owns the process-level registry and lifecycle service for
// encrypted workspace runtimes.
package workspaces

import (
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type State struct {
	Registry        *workspacelifecycle.Registry[workspaceruntime.Port]
	Lifecycle       *workspacelifecycle.Service[workspaceruntime.Port]
	MoveDatabase    func(string, string) error
	PublishDatabase func(string, string) error
	OpenRuntime     func(string, string, string) (workspaceruntime.Port, error)
}
