// Package operations defines the workspace boundary's operation-lifecycle port.
package operations

import (
	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
)

type Port interface {
	CommandRequestRuntime() *commandrequests.Runtime
	SetCommandRequestRuntime(*commandrequests.Runtime)
	FileTransferRuntime() *transferapp.Runtime
	SetFileTransferRuntime(*transferapp.Runtime)
	FileTransferLifecycle() *transferapp.Lifecycle
}
