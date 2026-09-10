package api

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
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
	return s.observation.VaultRequestStore(ctx, runtime)
}

func (s *Server) vaultRequestHTTPScope(w http.ResponseWriter) (vaultrequests.HTTPScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return vaultrequests.HTTPScope{}, false
	}
	return vaultrequests.HTTPScope{
		MCPStarted: runtime.IsMCPStarted,
		Runtime: func(ctx context.Context) (*vaultrequests.Runtime, error) {
			return s.vaultRequestRuntime(ctx, runtime)
		},
	}, true
}

func (s *Server) vaultRequestRuntime(ctx context.Context, runtime *databaseRuntime) (*vaultrequests.Runtime, error) {
	if s == nil || runtime == nil || runtime.Storage.Database == nil {
		return nil, vaultrequests.ErrRuntimeUnavailable
	}
	actions, err := s.vaultActionApplication(runtime)
	if err != nil {
		return nil, err
	}
	owner, err := vaultrequests.NewRuntime(vaultrequests.RuntimeDependencies{
		Store:           s.vaultRequestStore(ctx, runtime),
		Mutations:       vaultRequestMutationPort{server: s, runtime: runtime},
		Prepare:         actions.Prepare,
		AuthorizeOutput: actions.AuthorizeOutput,
		AllowRequest: func(tokenID int64) bool {
			return s.controlState.VaultRequestLimiter != nil && s.controlState.VaultRequestLimiter.Allow(
				"vault-request:"+runtime.ID+":"+strconv.FormatInt(tokenID, 10),
			)
		},
		Execute:    actions.Execute,
		Compensate: actions.Compensate,
		RepairProjection: func(ctx context.Context, id int64) error {
			if err := s.observation.SyncVaultActionRequest(ctx, runtime, id); err != nil {
				log.Printf("Vault request history projection repair failed request=%d error=%v", id, err)
			}
			return nil
		},
		RedactError: func(ctx context.Context, err error) string {
			return s.redactForPersistence(ctx, runtime, err.Error())
		},
		IsStale:    actions.IsStale,
		MCPStarted: runtime.IsMCPStarted,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Vault request runtime: %w", err)
	}
	return owner, nil
}
