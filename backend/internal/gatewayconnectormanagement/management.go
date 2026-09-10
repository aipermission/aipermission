// Package gatewayconnectormanagement is the connector catalog and target-management contract consumed by the gateway.
package gatewayconnectormanagement

import (
	applicationmanagement "github.com/aipermission/aipermission/backend/internal/applicationconnectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

var (
	New                                = applicationmanagement.New
	ValidateTransport                  = applicationmanagement.ValidateTransport
	NewCombinedMutationHTTPHandler     = connectormanagement.NewCombinedMutationHTTPHandler
	NewHTTPHandlers                    = connectormanagement.NewHTTPHandlers
	NewHostPingHTTPHandler             = connectormanagement.NewHostPingHTTPHandler
	NewProfileBackupHTTPHandler        = connectormanagement.NewProfileBackupHTTPHandler
	NewProfileDeletionHTTPHandler      = connectormanagement.NewProfileDeletionHTTPHandler
	NewProfileMutationHTTPHandler      = connectormanagement.NewProfileMutationHTTPHandler
	NewProfileTestingHTTPHandler       = connectormanagement.NewProfileTestingHTTPHandler
	NewProvisioningHTTPHandler         = connectormanagement.NewProvisioningHTTPHandler
	NewTargetMutationHTTPHandler       = connectormanagement.NewTargetMutationHTTPHandler
	NormalizeTargetConfig              = connectormanagement.NormalizeTargetConfig
	RuntimeCredentialPorts             = connectormanagement.RuntimeCredentialPorts
	RuntimeCredentialPreparation       = connectormanagement.RuntimeCredentialPreparation
	NewStore                           = connectortargets.NewStore
	NewTxStore                         = connectortargets.NewTxStore
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

type Component = applicationmanagement.Component
type Dependencies = applicationmanagement.Dependencies
type CredentialResourceDependencies = applicationmanagement.CredentialResourceDependencies
type AuditAppender = connectormanagement.AuditAppender
type ConnectionTestResponse = connectormanagement.ConnectionTestResponse
type CreateTargetRequest = connectormanagement.CreateTargetRequest
type CreateTargetWithProfileRequest = connectormanagement.CreateTargetWithProfileRequest
type CredentialBoundary = connectormanagement.CredentialBoundary
type CredentialCanonicalizer = connectormanagement.CredentialCanonicalizer
type CredentialPreparationPorts = connectormanagement.CredentialPreparationPorts
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
type InvalidateActionRequestsForTargetInput = connectortargets.InvalidateActionRequestsForTargetInput
type InvalidateActionRequestsForTargetResult = connectortargets.InvalidateActionRequestsForTargetResult
type ListTargetsFilter = connectortargets.ListTargetsFilter
type RuntimeSurface = connectortargets.RuntimeSurface
type Store = connectortargets.Store
type Target = connectortargets.Target
type ValidationError = connectortargets.ValidationError
