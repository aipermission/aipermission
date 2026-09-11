// Package operations defines the workspace boundary's operation-lifecycle port.
package operations

import (
	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

type Port interface {
	CommandRequestRuntime() *commandrequests.Runtime
	SetCommandRequestRuntime(*commandrequests.Runtime)
	FileTransferRuntime() *transferapp.Runtime
	SetFileTransferRuntime(*transferapp.Runtime)
	FileTransferLifecycle() *transferapp.Lifecycle
	ActionWorkflow() *actions.Runtime
	ActionWorkflowOrCreate(func() (*actions.Runtime, error)) (*actions.Runtime, error)
	ProjectVaultOrCreate(func() (*projectvault.Runtime, error)) (*projectvault.Runtime, error)
}
