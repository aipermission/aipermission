// Package gatewayconnectorapi exposes connector adapter and gateway-port contracts to the composition boundary.
package gatewayconnectorapi

import (
	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type ActionRuntime = connectorapi.ActionRuntime
type Adapter = connectorapi.Adapter
type ConnectorDataRuntime = connectorapi.ConnectorDataRuntime
type ConsoleRestartResult = connectorapi.ConsoleRestartResult
type CredentialCanonicalizer = connectorapi.CredentialCanonicalizer
type CredentialProfileLifecycleAdapter = connectorapi.CredentialProfileLifecycleAdapter
type CredentialProfileTester = connectorapi.CredentialProfileTester
type CredentialResourceAdapter = connectorapi.CredentialResourceAdapter
type CredentialResourceRuntime = connectorapi.CredentialResourceRuntime
type DraftTester = connectorapi.DraftTester
type ErrorPresenter = connectorapi.ErrorPresenter
type FileTransferAdapter = connectorapi.FileTransferAdapter
type LiveConsoleAdapter = connectorapi.LiveConsoleAdapter
type LiveConsoleEnvironmentPlan = connectorapi.LiveConsoleEnvironmentPlan
type LiveConsoleHTTPRuntime = connectorapi.LiveConsoleHTTPRuntime
type LiveConsolePeerIdentityAdapter = connectorapi.LiveConsolePeerIdentityAdapter
type LiveConsoleRestartResult = connectorapi.LiveConsoleRestartResult
type LiveConsoleRuntime = connectorapi.LiveConsoleRuntime
type LiveConsoleTargetAdapter = connectorapi.LiveConsoleTargetAdapter
type LiveConsoleTransportAdapter = connectorapi.LiveConsoleTransportAdapter
type Registry = connectorapi.Registry
type RuntimeAdapter = connectorapi.RuntimeAdapter
type TCPTransportAdapter = connectorapi.TCPTransportAdapter
type TargetDeleter = connectorapi.TargetDeleter
type TargetLifecycleRuntime = connectorapi.TargetLifecycleRuntime
type TargetOperationRunner = connectorapi.TargetOperationRunner
type TransferAuthorization = connectorapi.TransferAuthorization
type TransferBatch = connectorapi.TransferBatch
type ActionResponse = actions.Response
type ActionRequest = connectortargets.ActionRequest
