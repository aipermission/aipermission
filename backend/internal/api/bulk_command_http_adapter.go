package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
)

func (s *Server) bulkCommandHTTPScope(w http.ResponseWriter) (*gatewayaccess.CommandBulkHTTPRuntime, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	if runtime.OperationsPort().CommandRequestRuntime() == nil {
		writeInternalError(w)
		return nil, false
	}
	return &gatewayaccess.CommandBulkHTTPRuntime{
		Requests: runtime.OperationsPort().CommandRequestRuntime(),
		Sessions: runtime.ConnectorPort().ConsoleSessionManager(),
		Principal: func() (gatewayaccess.Principal, error) {
			return localExecutionPrincipal(runtime)
		},
		ResolveTarget: func(ctx context.Context, runtimeID int64) (gatewayaccess.CommandBulkTarget, error) {
			target, err := s.bulkConsoleTarget(ctx, runtime, runtimeID)
			if errors.Is(err, connectormgmt.ErrTargetProfileNotFound) ||
				errors.Is(err, connectormgmt.ErrTargetNotFound) ||
				errors.Is(err, connectormgmt.ErrRuntimeSurfaceNotFound) ||
				errors.Is(err, connectormgmt.ErrInvalidTargetRef) {
				return gatewayaccess.CommandBulkTarget{}, gatewayaccess.ErrBulkTargetNotFound
			}
			return target, err
		},
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, gatewayaccess.CommandBulkAuditAppender) error) error {
			return s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
				return mutate(tx, gatewayaccess.CommandBulkAuditAppender(appendAudit))
			})
		},
		PresentError: func(ctx context.Context, runtimeID int64, err error) string {
			adapter := s.consoleErrorPresenter(ctx, runtime, runtimeID)
			return connectorapi.PresentedErrorMessage(adapter, "command execution failed", err)
		},
		InitialTimeout: mcpInitialExecTimeout,
	}, true
}

func (s *Server) bulkConsoleTarget(ctx context.Context, runtime databaseRuntime, runtimeID int64) (gatewayaccess.CommandBulkTarget, error) {
	targetRef, err := liveConsoleTargetRefForRuntimeID(ctx, runtime, runtimeID)
	if err != nil {
		return gatewayaccess.CommandBulkTarget{}, err
	}
	target, profile, err := connectormgmt.NewStore(runtime.StoragePort().DatabaseHandle()).ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return gatewayaccess.CommandBulkTarget{}, err
	}
	actionAdapter, ok := s.connectorAPIAdapterFor(target.ConnectorKind).(connectorapi.LiveConsoleAdapter)
	if !ok || strings.TrimSpace(actionAdapter.LiveConsoleActionName()) == "" {
		return gatewayaccess.CommandBulkTarget{}, connectormgmt.ErrInvalidTargetRef
	}
	name := target.Name
	if adapter := s.connectorLiveConsoleTargetAdapterFor(target.ConnectorKind); adapter != nil {
		metadata := adapter.LiveConsoleTargetMetadata(connectors.TargetView{
			ID: target.ID, ConnectorKind: target.ConnectorKind, Name: target.Name, Config: target.Config,
		}, connectors.CredentialProfileView{
			ID: profile.ID, TargetID: profile.TargetID, ConnectorKind: profile.ConnectorKind,
			Kind: profile.Kind, Label: profile.Label, Public: profile.Public,
		})
		if label, _ := metadata["label"].(string); strings.TrimSpace(label) != "" {
			name = strings.TrimSpace(label)
		}
	}
	return gatewayaccess.CommandBulkTarget{RuntimeID: runtimeID, Name: name}, nil
}

func (s *Server) consoleErrorPresenter(ctx context.Context, runtime databaseRuntime, runtimeID int64) any {
	targetRef, err := liveConsoleTargetRefForRuntimeID(ctx, runtime, runtimeID)
	if err != nil {
		return nil
	}
	target, _, err := connectormgmt.NewStore(runtime.StoragePort().DatabaseHandle()).ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return nil
	}
	return s.connectorAPIAdapterFor(target.ConnectorKind)
}
