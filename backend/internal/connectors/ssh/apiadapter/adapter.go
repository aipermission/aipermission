// Package apiadapter registers the SSH connector's gateway adapter.
//
// The generic API package owns routing, auth, permission, approval, history,
// and audit. This package owns SSH-specific runtime behavior.
package apiadapter

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter/management"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter/running"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter/runtimeactions"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter/transport"
)

type adapter struct {
	management.Management
	running.Running
	runtimeactions.RuntimeActions
	transport.Transport
}

func New() connectorapi.Adapter {
	return adapter{}
}

var (
	_ connectorapi.CommandTransportAdapter           = adapter{}
	_ connectorapi.CredentialCanonicalizer           = adapter{}
	_ connectorapi.CredentialProfileLifecycleAdapter = adapter{}
	_ connectorapi.CredentialProfileTester           = adapter{}
	_ connectorapi.CredentialResourceAdapter         = adapter{}
	_ connectorapi.DraftTester                       = adapter{}
	_ connectorapi.ErrorPresenter                    = adapter{}
	_ connectorapi.FileTransferAdapter               = adapter{}
	_ connectorapi.LiveConsoleAdapter                = adapter{}
	_ connectorapi.LiveConsolePeerIdentityAdapter    = adapter{}
	_ connectorapi.LiveConsoleTargetAdapter          = adapter{}
	_ connectorapi.LiveConsoleTransportAdapter       = adapter{}
	_ connectorapi.RouteRegistrar                    = adapter{}
	_ connectorapi.RuntimeAdapter                    = adapter{}
	_ connectorapi.TargetDeleter                     = adapter{}
	_ connectorapi.TargetOperationRunner             = adapter{}
	_ connectorapi.TCPTransportAdapter               = adapter{}
)
