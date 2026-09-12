package gatewayinfrastructure

import (
	"context"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func (application *ConnectorManagementApplication) ResolveBulkCommandTarget(
	ctx context.Context,
	handle *WorkspaceHandle,
	connectorKinds []string,
	runtimeID int64,
) (gatewayoperations.CommandBulkTarget, error) {
	if application == nil || application.runtime == nil || handle == nil {
		return gatewayoperations.CommandBulkTarget{}, gatewayoperations.ErrBulkTargetNotFound
	}
	targetRef, err := application.runtime.ResolveLiveConsoleTarget(ctx, handle, connectorKinds, runtimeID)
	if err != nil {
		return gatewayoperations.CommandBulkTarget{}, presentBulkTargetError(err)
	}
	target, profile, err := application.Catalog(handle).ResolveActionTarget(ctx, targetRef)
	if err != nil {
		return gatewayoperations.CommandBulkTarget{}, presentBulkTargetError(err)
	}
	if _, ok := application.runtime.LiveConsoleActionName(target.ConnectorKind); !ok {
		return gatewayoperations.CommandBulkTarget{}, gatewayoperations.ErrBulkTargetNotFound
	}
	name := target.Name
	metadata := application.runtime.LiveConsoleTargetMetadata(target.ConnectorKind, connectors.TargetView{
		ID: target.ID, ConnectorKind: target.ConnectorKind, Name: target.Name, Config: target.Config,
	}, connectors.CredentialProfileView{
		ID: profile.ID, TargetID: profile.TargetID, ConnectorKind: profile.ConnectorKind,
		Kind: profile.Kind, Label: profile.Label, Public: profile.Public,
	})
	if label, _ := metadata["label"].(string); strings.TrimSpace(label) != "" {
		name = strings.TrimSpace(label)
	}
	return gatewayoperations.CommandBulkTarget{RuntimeID: runtimeID, Name: name}, nil
}

func (application *ConnectorManagementApplication) ConsoleErrorPresenter(
	ctx context.Context,
	handle *WorkspaceHandle,
	connectorKinds []string,
	runtimeID int64,
) connectorapi.ErrorPresenter {
	if application == nil || application.runtime == nil || handle == nil {
		return nil
	}
	targetRef, err := application.runtime.ResolveLiveConsoleTarget(ctx, handle, connectorKinds, runtimeID)
	if err != nil {
		return nil
	}
	target, _, err := application.Catalog(handle).ResolveActionTarget(ctx, targetRef)
	if err != nil {
		return nil
	}
	return application.runtime.ErrorPresenter(target.ConnectorKind)
}

func presentBulkTargetError(err error) error {
	if connectormgmt.IsTargetProfileNotFound(err) || connectormgmt.IsTargetNotFound(err) ||
		connectormgmt.IsRuntimeSurfaceNotFound(err) || connectormgmt.IsInvalidTargetRef(err) {
		return gatewayoperations.ErrBulkTargetNotFound
	}
	return err
}
