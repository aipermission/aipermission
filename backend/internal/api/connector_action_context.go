package api

import (
	"crypto/sha256"
	"encoding/hex"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func connectorActionApprovalSnapshots(token gatewayaccess.Token, permission connectormgmt.ActionPermission) (actions.ApprovalTokenSnapshot, actions.ApprovalPermissionSnapshot) {
	return actions.ApprovalTokenSnapshot{
			ID: token.ID, ExpiresAt: token.ExpiresAt, RevokedAt: token.RevokedAt,
		}, actions.ApprovalPermissionSnapshot{
			Rule: string(permission.ExecutionRule), ExpiresAt: permission.ExpiresAt,
			ProjectID: permission.ProjectID, ProjectName: permission.ProjectName, ProjectSlug: permission.ProjectSlug,
		}
}
