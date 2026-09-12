package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func (s *Server) bulkCommandHTTPScope(w http.ResponseWriter) (*gatewayoperations.CommandBulkHTTPRuntime, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	requests, err := s.commandRuntime(runtime)
	if err != nil {
		writeInternalError(w)
		return nil, false
	}
	return s.operationsOwner.CommandBulkRuntime(runtime, gatewayoperations.CommandBulkHTTPRuntime{
		Requests: requests,
		Principal: func() (gatewayaccess.Principal, error) {
			return s.localExecutionPrincipal(runtime)
		},
		ResolveTarget: func(ctx context.Context, runtimeID int64) (gatewayoperations.CommandBulkTarget, error) {
			target, err := s.bulkConsoleTarget(ctx, runtime, runtimeID)
			if connectormgmt.IsTargetProfileNotFound(err) ||
				connectormgmt.IsTargetNotFound(err) ||
				connectormgmt.IsRuntimeSurfaceNotFound(err) ||
				connectormgmt.IsInvalidTargetRef(err) {
				return gatewayoperations.CommandBulkTarget{}, gatewayoperations.ErrBulkTargetNotFound
			}
			return target, err
		},
		PresentError: func(ctx context.Context, runtimeID int64, err error) string {
			adapter := s.consoleErrorPresenter(ctx, runtime, runtimeID)
			return connectorapi.PresentedErrorMessage(adapter, "command execution failed", err)
		},
		InitialTimeout: mcpInitialExecTimeout,
	})
}

func (s *Server) bulkConsoleTarget(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, runtimeID int64) (gatewayoperations.CommandBulkTarget, error) {
	targetRef, err := s.liveConsoleTargetRefForRuntimeID(ctx, runtime, runtimeID)
	if err != nil {
		return gatewayoperations.CommandBulkTarget{}, err
	}
	target, profile, err := s.connectorCatalog(runtime).ResolveActionTarget(ctx, targetRef)
	if err != nil {
		return gatewayoperations.CommandBulkTarget{}, err
	}
	if _, ok := s.connectorRuntime.LiveConsoleActionName(target.ConnectorKind); !ok {
		return gatewayoperations.CommandBulkTarget{}, connectormgmt.InvalidTargetRefError()
	}
	name := target.Name
	metadata := s.connectorRuntime.LiveConsoleTargetMetadata(target.ConnectorKind, connectors.TargetView{
		ID: target.ID, ConnectorKind: target.ConnectorKind, Name: target.Name, Config: target.Config,
	}, connectors.CredentialProfileView{
		ID: profile.ID, TargetID: profile.TargetID, ConnectorKind: profile.ConnectorKind,
		Kind: profile.Kind, Label: profile.Label, Public: profile.Public,
	})
	if metadata != nil {
		if label, _ := metadata["label"].(string); strings.TrimSpace(label) != "" {
			name = strings.TrimSpace(label)
		}
	}
	return gatewayoperations.CommandBulkTarget{RuntimeID: runtimeID, Name: name}, nil
}

func (s *Server) consoleErrorPresenter(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, runtimeID int64) connectorapi.ErrorPresenter {
	targetRef, err := s.liveConsoleTargetRefForRuntimeID(ctx, runtime, runtimeID)
	if err != nil {
		return nil
	}
	target, _, err := s.connectorCatalog(runtime).ResolveActionTarget(ctx, targetRef)
	if err != nil {
		return nil
	}
	return s.connectorRuntime.ErrorPresenter(target.ConnectorKind)
}
