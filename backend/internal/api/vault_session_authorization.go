package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/console"
)

func vaultSessionObserveAuthorized(
	ctx context.Context,
	runtime *databaseRuntime,
	tokenID int64,
	sessionID int64,
	generation int64,
	expectedRuntimeID int64,
	requireEnvironment bool,
) bool {
	if runtime == nil || sessionID < 1 || generation < 1 {
		return false
	}
	var runtimeID int64
	var environmentHash, approvalHash, status string
	err := runtime.database.QueryRowContext(ctx, `
		SELECT runtime_id, environment_content_hash, approval_context_hash, status
		FROM console_sessions
		WHERE id = ? AND generation = ?`,
		sessionID, generation,
	).Scan(&runtimeID, &environmentHash, &approvalHash, &status)
	if err != nil || (expectedRuntimeID > 0 && runtimeID != expectedRuntimeID) {
		return false
	}
	if environmentHash == "" {
		return !requireEnvironment
	}
	if status != "connecting" && status != "connected" {
		return false
	}
	principal, err := tokenExecutionPrincipal(runtime, tokenID)
	if err != nil {
		return false
	}
	return runtime.vaultLeases.Authorize(ctx, principal, console.SessionAuthorization{
		Handle: console.SessionHandle{
			ID: sessionID, RuntimeID: runtimeID, Generation: generation,
		},
		EnvironmentContentHash: environmentHash,
		ApprovalContextHash:    approvalHash,
	}, console.OperationObserve) == nil
}
