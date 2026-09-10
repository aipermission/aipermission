package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/observability"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type vaultRequestMutationPort struct {
	server  *Server
	runtime *databaseRuntime
}

func (p vaultRequestMutationPort) WithMutation(
	ctx context.Context,
	actor string,
	tokenID *int64,
	runtimeID int64,
	action string,
	payload func() any,
	mutate func(*sql.Tx) error,
) error {
	if p.server == nil || p.runtime == nil {
		return vaultrequests.ErrRuntimeUnavailable
	}
	return p.server.withAuditedMutation(ctx, p.runtime, actor, tokenID, runtimeID, action, payload, mutate)
}

func (p vaultRequestMutationPort) Observe(
	ctx context.Context,
	actor string,
	tokenID *int64,
	runtimeID int64,
	action string,
	payload any,
) {
	if p.server != nil && p.runtime != nil {
		p.server.writeObservationAudit(ctx, p.runtime, actor, tokenID, runtimeID, action, payload)
	}
}

func (s *Server) vaultRequestStore(ctx context.Context, runtime *databaseRuntime) *vaultrequests.Store {
	redact := s.prepareAuditRedactor(ctx, runtime)
	return vaultrequests.NewStore(runtime.database).WithMutationHook(func(ctx context.Context, executor vaultrequests.Executor, item vaultrequests.Request) error {
		event, err := observability.BuildEvent(ctx, executor, observability.BuildInput{
			ActorType: "gateway",
			TokenID:   int64Ptr(item.TokenID),
			RuntimeID: valueOrZero(item.RuntimeID),
			Action:    "vault.action_request." + item.Status,
			Payload:   vaultrequests.RequestAuditPayload(item, item.UserNote),
			Redact:    redact,
		})
		if err != nil {
			return err
		}
		_, err = (observability.Store{}).Append(ctx, executor, event)
		return err
	})
}

func (s *Server) vaultRequestRuntime(ctx context.Context, runtime *databaseRuntime) (*vaultrequests.Runtime, error) {
	if s == nil || runtime == nil || runtime.database == nil {
		return nil, vaultrequests.ErrRuntimeUnavailable
	}
	owner, err := vaultrequests.NewRuntime(vaultrequests.RuntimeDependencies{
		Store:     s.vaultRequestStore(ctx, runtime),
		Mutations: vaultRequestMutationPort{server: s, runtime: runtime},
		Prepare: func(ctx context.Context, tokenID int64, projectRef, actionName string, input map[string]any) (vaultrequests.PreparedAction, error) {
			project, err := resolveProjectRef(ctx, runtime, projectRef)
			if errors.Is(err, projectstore.ErrNotFound) {
				return vaultrequests.PreparedAction{}, vaultrequests.ErrProjectNotFound
			}
			if err != nil {
				return vaultrequests.PreparedAction{}, err
			}
			approval, contextHash, normalizedInput, err := buildVaultApprovalContext(
				ctx, s, runtime, tokenID, project, actionName, input,
			)
			if err != nil {
				return vaultrequests.PreparedAction{}, vaultrequests.PreparationError{Err: err}
			}
			return vaultrequests.PreparedAction{
				ProjectID: project.ID, RuntimeID: approval.RuntimeID, Input: normalizedInput,
				ApprovalContext: approval, ApprovalContextHash: contextHash,
				RunImmediately: approval.ExecutionRule == accesscontrol.RuleAlwaysRun,
			}, nil
		},
		AuthorizeOutput: func(ctx context.Context, item vaultrequests.Request) bool {
			return currentVaultPollAuthorization(ctx, s, runtime, item)
		},
		AllowRequest: func(tokenID int64) bool {
			return s.vaultRequestLimiter != nil && s.vaultRequestLimiter.Allow(
				"vault-request:"+runtime.id+":"+strconv.FormatInt(tokenID, 10),
			)
		},
		Execute: func(ctx context.Context, item vaultrequests.Request) (any, error) {
			return executeVaultAction(ctx, s, runtime, item)
		},
		Compensate: func(ctx context.Context, item vaultrequests.Request, output any) error {
			return compensateVaultActionEffect(ctx, runtime, item, output)
		},
		RepairProjection: func(ctx context.Context, id int64) error {
			if err := history.NewStore(runtime.database).SyncVaultActionRequest(ctx, id); err != nil {
				log.Printf("Vault request history projection repair failed request=%d error=%v", id, err)
			}
			return nil
		},
		RedactError: func(ctx context.Context, err error) string {
			return s.redactForPersistence(ctx, runtime, err.Error())
		},
		IsStale:    isVaultContextDrift,
		MCPStarted: runtime.isMCPStarted,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Vault request runtime: %w", err)
	}
	return owner, nil
}

func compensateVaultActionEffect(ctx context.Context, runtime *databaseRuntime, request vaultrequests.Request, output any) error {
	payload, _ := output.(map[string]any)
	switch request.ActionName {
	case vaultrequests.ActionRestartSession:
		sessionID := vaultJSONInt(payload["session_id"])
		if sessionID < 1 {
			return nil
		}
		runtimeID := vaultJSONInt(payload["runtime_id"])
		generation := vaultJSONInt(payload["session_generation"])
		if runtimeID > 0 && generation > 0 {
			runtime.vaultLeases.RevokeSession(console.SessionHandle{
				ID: sessionID, RuntimeID: runtimeID, Generation: generation,
			})
		}
		var cleanupErrors []error
		if generation > 0 {
			if err := vaultsessions.NewPersistence(runtime.database).Revoke(ctx, sessionID, generation); err != nil {
				cleanupErrors = append(cleanupErrors, err)
			}
		}
		principal, err := localExecutionPrincipal(runtime)
		if err != nil {
			return err
		}
		if err := runtime.consoleSessions.Close(ctx, principal, sessionID); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
		return errors.Join(cleanupErrors...)
	case vaultrequests.ActionGenerateItem:
		itemPayload, _ := payload["item"].(map[string]any)
		itemID := vaultJSONInt(itemPayload["item_id"])
		valueVersion := vaultJSONInt(itemPayload["value_version"])
		metadataRevision := vaultJSONInt(itemPayload["metadata_revision"])
		if itemID < 1 || valueVersion < 1 || metadataRevision < 1 {
			return nil
		}
		release, err := runtime.vaultDelivery.acquireExclusive(ctx)
		if err != nil {
			return err
		}
		defer release()
		store, err := projectvault.NewStore(runtime.database, runtime.vault, runtime.workspaceUUID)
		if err != nil {
			return err
		}
		return store.Delete(ctx, itemID, valueVersion, metadataRevision)
	default:
		return nil
	}
}
