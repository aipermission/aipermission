package gatewayinfrastructure

import (
	"context"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
	gatewaytransfer "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
)

type FileTransferConnectorPorts struct {
	ConnectorKind      string
	Gateway            connectorapi.FileTransferGateway
	Runtime            connectorapi.TransferRuntime
	CredentialBoundary gatewaytransfer.CredentialBoundary
}
type FileTransferConnectorPortsResolver func(context.Context, int64) (FileTransferConnectorPorts, error)

type connectorSecretAccessor struct {
	values   map[string]any
	boundary interface{ Add(...string) }
}

func (accessor connectorSecretAccessor) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := accessor.values[name]
	if !ok || value == nil {
		return "", fmt.Errorf("%w: %q", connectors.ErrSecretNotFound, name)
	}
	text := fmt.Sprint(value)
	if accessor.boundary != nil {
		accessor.boundary.Add(text)
	}
	return text, nil
}

func (accessor connectorSecretAccessor) RegisterSensitiveValue(value string) {
	if accessor.boundary != nil {
		accessor.boundary.Add(value)
	}
}

func (application *ConnectorManagementApplication) FileTransferPorts(
	ctx context.Context,
	handle *WorkspaceHandle,
	runtimeID int64,
) (FileTransferConnectorPorts, error) {
	if application == nil || application.runtime == nil || handle == nil {
		return FileTransferConnectorPorts{}, errors.New("file transfer connector runtime is unavailable")
	}
	target, _, _, err := application.Catalog(handle).TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return FileTransferConnectorPorts{}, err
	}
	workspace, ok := application.runtime.workspace(handle, true, nil, ConnectorTargetWorkflowPorts{})
	if !ok {
		return FileTransferConnectorPorts{}, errors.New("file transfer connector workspace is unavailable")
	}
	boundary := gatewaytransfer.NewCredentialBoundary(nil)
	runtime := connectorports.TransferRuntimeWithSecretAccessor(workspace, target.ConnectorKind, func(secrets map[string]any) connectors.SecretAccessor {
		boundary.AddStructured(secrets)
		return connectorSecretAccessor{values: secrets, boundary: boundary}
	})
	return FileTransferConnectorPorts{
		ConnectorKind:      target.ConnectorKind,
		Gateway:            application.runtime.ports.FileTransferGateway(workspace, target.ConnectorKind),
		Runtime:            runtime,
		CredentialBoundary: boundary,
	}, nil
}
