package vaultsessions

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

type LeaseAuthorizer interface {
	Authorize(context.Context, executionprincipal.Principal, console.SessionAuthorization, console.SessionOperation) error
}

type Observer struct {
	database sqldb.Executor
	leases   LeaseAuthorizer
}

type ObserveRequest struct {
	SessionID          int64
	SessionGeneration  int64
	ExpectedRuntimeID  int64
	RequireEnvironment bool
}

func NewObserver(database sqldb.Executor, leases LeaseAuthorizer) *Observer {
	return &Observer{database: database, leases: leases}
}

func (o *Observer) Authorized(
	ctx context.Context,
	principal executionprincipal.Principal,
	request ObserveRequest,
) bool {
	if o == nil || o.database == nil || o.leases == nil || !principal.IsMCPToken() ||
		request.SessionID < 1 || request.SessionGeneration < 1 {
		return false
	}
	var runtimeID int64
	var environmentHash, approvalHash, status string
	err := o.database.QueryRowContext(ctx, `
		SELECT runtime_id, environment_content_hash, approval_context_hash, status
		FROM console_sessions
		WHERE id = ? AND generation = ?`,
		request.SessionID, request.SessionGeneration,
	).Scan(&runtimeID, &environmentHash, &approvalHash, &status)
	if err != nil || (request.ExpectedRuntimeID > 0 && runtimeID != request.ExpectedRuntimeID) {
		return false
	}
	if environmentHash == "" {
		return !request.RequireEnvironment
	}
	if status != "connecting" && status != "connected" {
		return false
	}
	return o.leases.Authorize(ctx, principal, console.SessionAuthorization{
		Handle: console.SessionHandle{
			ID: request.SessionID, RuntimeID: runtimeID, Generation: request.SessionGeneration,
		},
		EnvironmentContentHash: environmentHash,
		ApprovalContextHash:    approvalHash,
	}, console.OperationObserve) == nil
}
