package api

import (
	"crypto/sha256"
	"encoding/hex"

	domainactions "github.com/aipermission/aipermission/backend/internal/actions"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func connectorActionApprovalSnapshots(token gatewayaccess.Token, permission connectormgmt.ActionPermission) (domainactions.ApprovalTokenSnapshot, domainactions.ApprovalPermissionSnapshot) {
	return domainactions.ApprovalTokenSnapshot{
			ID: token.ID, ExpiresAt: token.ExpiresAt, RevokedAt: token.RevokedAt,
		}, domainactions.ApprovalPermissionSnapshot{
			Rule: string(permission.ExecutionRule), ExpiresAt: permission.ExpiresAt,
			ProjectID: permission.ProjectID, ProjectName: permission.ProjectName, ProjectSlug: permission.ProjectSlug,
		}
}
