// Package gatewayvault exposes project, secret, and Vault-session application contracts to the gateway.
package gatewayvault

import (
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

var (
	ErrInvalidatorUnavailable = vaultsessions.ErrInvalidatorUnavailable
)

const (
	ActionGenerateItem = vaultrequests.ActionGenerateItem
)

type ProjectScope = projects.Scope
type ProjectVaultHTTPScope = projectvault.HTTPScope
type SessionMutationScope = projectvault.SessionMutationScope
type SessionReference = projectvault.SessionReference
type SessionSelection = projectvault.SessionSelection
type VaultApprovalHTTPScope = vaultrequests.HTTPScope
type VaultMCPHTTPScope = vaultrequests.MCPHTTPScope
type VaultRequestApplication = vaultrequests.Application
type Invalidator = vaultsessions.Invalidator
type InvalidatorDependencies = vaultsessions.InvalidatorDependencies
type VaultSessionReference = vaultsessions.Reference
type RequestInvalidator = vaultsessions.RequestInvalidator
