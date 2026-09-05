package api

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/expirypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func connectorActionApprovalSnapshots(token tokens.Token, permission connectortargets.ActionPermission) (actions.ApprovalTokenSnapshot, actions.ApprovalPermissionSnapshot) {
	return actions.ApprovalTokenSnapshot{
			ID: token.ID, ExpiresAt: token.ExpiresAt, RevokedAt: token.RevokedAt,
		}, actions.ApprovalPermissionSnapshot{
			Rule: string(permission.ExecutionRule), ExpiresAt: permission.ExpiresAt,
			ProjectID: permission.ProjectID, ProjectName: permission.ProjectName, ProjectSlug: permission.ProjectSlug,
		}
}

func expired(value string, now time.Time) bool {
	return !expirypolicy.Active(value, now)
}
