package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/messagequeue"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

type actionTokenReader struct{ runtime *databaseRuntime }

func (r actionTokenReader) Get(ctx context.Context, tokenID int64, now time.Time) (actions.AuthorizationToken, error) {
	if r.runtime == nil || r.runtime.tokens == nil {
		return actions.AuthorizationToken{}, actions.ErrWorkflowUnavailable
	}
	token, err := r.runtime.tokens.Get(ctx, tokenID)
	if errors.Is(err, tokens.ErrNotFound) {
		return actions.AuthorizationToken{}, actions.ErrTokenNotFound
	}
	if err != nil {
		return actions.AuthorizationToken{}, err
	}
	return actions.AuthorizationToken{
		ID: token.ID, RevokedAt: token.RevokedAt, ExpiresAt: token.ExpiresAt,
		Active: tokens.Active(token.RevokedAt, token.ExpiresAt, now),
	}, nil
}

type actionDeliveryGate struct{ runtime *databaseRuntime }

func (g actionDeliveryGate) Acquire(ctx context.Context) (func(), error) {
	if g.runtime == nil {
		return nil, actions.ErrWorkflowUnavailable
	}
	return g.runtime.vaultDelivery.acquireDelivery(ctx)
}

type actionSealedRecords struct{ runtime *databaseRuntime }

func (p actionSealedRecords) SealActionRequest(requestID int64, envelope actions.ExecutionEnvelope) (string, error) {
	if p.runtime == nil || p.runtime.vault == nil {
		return "", actions.ErrWorkflowUnavailable
	}
	return recordcrypto.EncryptJSON(p.runtime.vault, p.runtime.workspaceUUID, recordcrypto.ConnectorActionRequest, requestID, envelope)
}

func (p actionSealedRecords) OpenActionRequest(requestID int64, sealed string) (actions.ExecutionEnvelope, error) {
	if p.runtime == nil || p.runtime.vault == nil {
		return actions.ExecutionEnvelope{}, actions.ErrWorkflowUnavailable
	}
	var envelope actions.ExecutionEnvelope
	if err := recordcrypto.DecryptJSON(p.runtime.vault, p.runtime.workspaceUUID, recordcrypto.ConnectorActionRequest, requestID, sealed, &envelope); err != nil {
		return actions.ExecutionEnvelope{}, err
	}
	return envelope, nil
}

func (p actionSealedRecords) OpenCredentialProfile(profileID int64, sealed string) (map[string]any, error) {
	secrets := map[string]any{}
	if sealed == "" {
		return secrets, nil
	}
	if p.runtime == nil || p.runtime.vault == nil {
		return nil, actions.ErrWorkflowUnavailable
	}
	if err := recordcrypto.DecryptJSON(p.runtime.vault, p.runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profileID, sealed, &secrets); err != nil {
		return nil, err
	}
	return secrets, nil
}

type actionMutationPort struct {
	server  *Server
	runtime *databaseRuntime
}

func (p actionMutationPort) WithMutation(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
	if p.server == nil || p.runtime == nil {
		return actions.ErrWorkflowUnavailable
	}
	return p.server.withAuditedMutation(ctx, p.runtime, actor, tokenID, runtimeID, action, payload, mutate)
}

func (p actionMutationPort) WithTransaction(ctx context.Context, mutate func(*sql.Tx, actions.AuditAppender) error) error {
	if p.server == nil || p.runtime == nil {
		return actions.ErrWorkflowUnavailable
	}
	return p.server.withAuditedTransaction(ctx, p.runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
		return mutate(tx, actions.AuditAppender(appendAudit))
	})
}

func (p actionMutationPort) Observe(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	if p.server != nil && p.runtime != nil {
		p.server.writeObservationAudit(ctx, p.runtime, actor, tokenID, runtimeID, action, payload)
	}
}

type actionRunningPort struct {
	server  *Server
	runtime *databaseRuntime
}

func (p actionRunningPort) SupportsRunning(prepared actions.PreparedRequest) bool {
	return p.server != nil && p.server.connectorActionSupportsRunning(prepared)
}

func (p actionRunningPort) FinishRunning(requestID int64, prepared actions.PreparedRequest, principal executionprincipal.Principal, handles connectors.ActionHandles) {
	if p.server != nil && p.runtime != nil {
		p.server.finishActiveConnectorActionRequest(p.runtime, requestID, prepared, principal, handles)
	}
}

func (s *Server) connectorActionWorkflow(runtime *databaseRuntime) (*actions.Runtime, error) {
	if s == nil || runtime == nil {
		return nil, actions.ErrWorkflowUnavailable
	}
	runtime.actionWorkflowMu.Lock()
	defer runtime.actionWorkflowMu.Unlock()
	if runtime.actionWorkflow != nil {
		return runtime.actionWorkflow, nil
	}
	redactor, err := s.connectorActionRedactor(runtime)
	if err != nil {
		return nil, err
	}
	workflow, err := actions.NewRuntime(actions.RuntimeDependencies{
		Database:    runtime.database,
		Tokens:      actionTokenReader{runtime: runtime},
		Registry:    runtime.connectorRegistry(),
		Targets:     newConnectorActionTargetResolver(runtime.database),
		IdentityKey: runtime.actionIdentityKey,
		Delivery:    actionDeliveryGate{runtime: runtime},
		MCPStarted:  runtime.isMCPStarted,
		Identity: func() (string, string, error) {
			if err := ensureRuntimeIdentity(runtime); err != nil {
				return "", "", err
			}
			return runtime.workspaceUUID, runtime.runtimeInstanceID, nil
		},
		Redactor:      redactor,
		SealedRecords: actionSealedRecords{runtime: runtime},
		Mutations:     actionMutationPort{server: s, runtime: runtime},
		Capabilities: func(kind string, dependencies []actions.ResolvedDependency) connectors.RuntimeCapabilityResolver {
			return connectorRuntimeCapabilitiesForAction(kind, s, runtime, dependencies)
		},
		RunningActions: actionRunningPort{server: s, runtime: runtime},
		EnqueueUserNote: func(ctx context.Context, tx *sql.Tx, tokenID int64, message string) error {
			return messagequeue.EnqueueUserNote(ctx, tx, tokenID, message)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize connector action workflow: %w", err)
	}
	runtime.actionWorkflow = workflow
	return workflow, nil
}
