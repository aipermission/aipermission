// Package operations defines the workspace boundary's operation-lifecycle port.
package operations

import transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"

type Port interface {
	FileTransferRuntime() *transferapp.Runtime
	SetFileTransferRuntime(*transferapp.Runtime)
	FileTransferLifecycle() *transferapp.Lifecycle
}
