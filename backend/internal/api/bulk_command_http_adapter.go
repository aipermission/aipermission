package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

func (s *Server) bulkCommandHTTPScope(w http.ResponseWriter) (*commandrequests.BulkHTTPRuntime, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	if runtime.Operations.CommandRequests == nil {
		writeInternalError(w)
		return nil, false
	}
	return &commandrequests.BulkHTTPRuntime{
		Requests: runtime.Operations.CommandRequests,
		Sessions: runtime.Connectors.ConsoleSessions,
		Principal: func() (executionprincipal.Principal, error) {
			return localExecutionPrincipal(runtime)
		},
		ResolveTarget: func(ctx context.Context, runtimeID int64) (commandrequests.BulkTarget, error) {
			target, err := s.bulkConsoleTarget(ctx, runtime, runtimeID)
			if errors.Is(err, connectortargets.ErrTargetProfileNotFound) ||
				errors.Is(err, connectortargets.ErrTargetNotFound) ||
				errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) ||
				errors.Is(err, connectortargets.ErrInvalidTargetRef) {
				return commandrequests.BulkTarget{}, commandrequests.ErrBulkTargetNotFound
			}
			return target, err
		},
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, commandrequests.BulkAuditAppender) error) error {
			return s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
				return mutate(tx, commandrequests.BulkAuditAppender(appendAudit))
			})
		},
		PresentError: func(ctx context.Context, runtimeID int64, err error) string {
			adapter := s.consoleErrorPresenter(ctx, runtime, runtimeID)
			return connectorapi.PresentedErrorMessage(adapter, "command execution failed", err)
		},
		InitialTimeout: mcpInitialExecTimeout,
	}, true
}

func (s *Server) bulkConsoleTarget(ctx context.Context, runtime *databaseRuntime, runtimeID int64) (commandrequests.BulkTarget, error) {
	targetRef, err := liveConsoleTargetRefForRuntimeID(ctx, runtime, runtimeID)
	if err != nil {
		return commandrequests.BulkTarget{}, err
	}
	target, profile, err := connectortargets.NewStore(runtime.Storage.Database).ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return commandrequests.BulkTarget{}, err
	}
	actionAdapter, ok := s.connectorAPIAdapterFor(target.ConnectorKind).(connectorapi.LiveConsoleAdapter)
	if !ok || strings.TrimSpace(actionAdapter.LiveConsoleActionName()) == "" {
		return commandrequests.BulkTarget{}, connectortargets.ErrInvalidTargetRef
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
	return commandrequests.BulkTarget{RuntimeID: runtimeID, Name: name}, nil
}

func (s *Server) consoleErrorPresenter(ctx context.Context, runtime *databaseRuntime, runtimeID int64) any {
	targetRef, err := liveConsoleTargetRefForRuntimeID(ctx, runtime, runtimeID)
	if err != nil {
		return nil
	}
	target, _, err := connectortargets.NewStore(runtime.Storage.Database).ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return nil
	}
	return s.connectorAPIAdapterFor(target.ConnectorKind)
}
