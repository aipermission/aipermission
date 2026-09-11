// Package workspaces owns the process-level registry and lifecycle service for
// encrypted workspace runtimes.
package workspaces

import (
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtimecontract"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type State struct {
	Registry        *workspacelifecycle.Registry[runtimecontract.Runtime]
	Lifecycle       *workspacelifecycle.Service[runtimecontract.Runtime]
	MoveDatabase    func(string, string) error
	PublishDatabase func(string, string) error
	OpenRuntime     func(string, string, string) (runtimecontract.Runtime, error)
}
