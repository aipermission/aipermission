// Package gatewayconnectormanagement is the connector catalog and target-management contract consumed by the gateway.
package gatewayconnectormanagement

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapproval"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

var (
	ErrCredentialProfileUpdateConflict = connectortargets.ErrCredentialProfileUpdateConflict
	ErrInvalidTargetRef                = connectortargets.ErrInvalidTargetRef
	ErrRuntimeSurfaceNotFound          = connectortargets.ErrRuntimeSurfaceNotFound
	ErrTargetNotFound                  = connectortargets.ErrTargetNotFound
	ErrTargetProfileNotFound           = connectortargets.ErrTargetProfileNotFound
	ErrTargetUpdateConflict            = connectortargets.ErrTargetUpdateConflict
)

const (
	ActionPermissionAlwaysRun        = connectortargets.ActionPermissionAlwaysRun
	ActionPermissionApprovalRequired = connectortargets.ActionPermissionApprovalRequired
)

type AuditAppender = connectormanagement.AuditAppender
type ConnectionTestResponse = connectormanagement.ConnectionTestResponse
type CreateTargetRequest = connectormanagement.CreateTargetRequest
type CreateTargetWithProfileRequest = connectormanagement.CreateTargetWithProfileRequest
type CredentialBoundary = connectormanagement.CredentialBoundary
type CredentialCanonicalizer = connectormanagement.CredentialCanonicalizer
type CredentialPreparationPorts = connectormanagement.CredentialPreparationPorts
type CredentialStorage = connectormanagement.CredentialStorage
type CredentialProfileInput = connectormanagement.CredentialProfileInput
type CredentialRuntimePorts = connectormanagement.CredentialRuntimePorts
type PreparedCredentialProfile = connectormanagement.PreparedCredentialProfile
type ProfileSummary = connectormanagement.ProfileSummary
type ProvisionRequest = connectormanagement.ProvisionRequest
type TargetLifecycleChange = connectormanagement.TargetLifecycleChange
type TargetResponse = connectormanagement.TargetResponse
type UpdateTargetRequest = connectormanagement.UpdateTargetRequest
type UpdateTargetWithProfileRequest = connectormanagement.UpdateTargetWithProfileRequest
type ActionPermission = connectortargets.ActionPermission
type ActionRequest = connectortargets.ActionRequest
type CredentialProfile = connectortargets.CredentialProfile
type RuntimeSurface = connectortargets.RuntimeSurface
type Target = connectortargets.Target
type ValidationError = connectortargets.ValidationError
type ConnectorApprovalItem = connectorapproval.Item
type ConnectorApprovalNoteRequest = connectorapproval.NoteRequest
type ConnectorApprovalScope = connectorapproval.Scope
type ConnectorApprovalWorkflow = connectorapproval.Workflow
