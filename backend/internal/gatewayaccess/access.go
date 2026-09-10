// Package gatewayaccess exposes authorization, approval, MCP, and security contracts to the gateway.
package gatewayaccess

import (
	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	"github.com/aipermission/aipermission/backend/internal/connectorapproval"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

var (
	NewCapabilityStore                         = accesscontrol.NewCapabilityStore
	NewAccessHTTPHandlers                      = accesscontrol.NewHTTPHandlers
	ProjectScopedSupportedConnectorPermissions = accesscontrol.ProjectScopedSupportedConnectorPermissions
	AnalyzeCommandPolicy                       = commandrequests.AnalyzePolicy
	NewBulkHTTPHandlers                        = commandrequests.NewBulkHTTPHandlers
	NewCommandHTTPHandlers                     = commandrequests.NewHTTPHandlers
	NewCommandWorkspaceRuntime                 = commandrequests.NewWorkspaceRuntime
	ErrBulkTargetNotFound                      = commandrequests.ErrBulkTargetNotFound
	ErrCommandRuntimeUnavailable               = commandrequests.ErrRuntimeUnavailable
	ConnectorApprovalItemForResponse           = connectorapproval.ItemForResponse
	ConnectorApprovalItemFromRequest           = connectorapproval.ItemFromRequest
	NewConnectorApprovalHTTPHandlers           = connectorapproval.NewHTTPHandlers
	ErrInvalidPrincipal                        = executionprincipal.ErrInvalid
	NewRuntimeInstanceID                       = executionprincipal.NewRuntimeInstanceID
	PrincipalLocalOperator                     = executionprincipal.LocalOperator
	PrincipalMCPToken                          = executionprincipal.MCPToken
	NewMCPActionHTTPHandlers                   = mcpconnector.NewActionHTTPHandlers
	NewMCPReadHTTPHandlers                     = mcpconnector.NewHTTPHandlers
	MCPResponseFromResult                      = mcpconnector.ResponseFromResult
	RuntimeKey                                 = runtimecontrol.Key
	NewMCPRuntimeHTTPHandlers                  = runtimecontrol.NewMCPHTTPHandlers
	NewSecurityHTTPHandlers                    = securitypolicy.NewHTTPHandlers
	RedactBasic                                = securitypolicy.RedactBasic
	ErrTokenNotFound                           = tokens.ErrNotFound
	HashToken                                  = tokens.HashToken
)

const (
	RuleAlwaysRun        = accesscontrol.RuleAlwaysRun
	VaultMetadataRead    = accesscontrol.VaultMetadataRead
	RunningAssistantHint = commandrequests.RunningAssistantHint
	CommandSourceMCP     = commandrequests.SourceMCP
	CommandSourceManual  = commandrequests.SourceManual
)

type AccessScope = accesscontrol.Scope
type CommandBulkAuditAppender = commandrequests.BulkAuditAppender
type CommandBulkHTTPRuntime = commandrequests.BulkHTTPRuntime
type CommandBulkTarget = commandrequests.BulkTarget
type CommandHTTPReader = commandrequests.HTTPReader
type CommandPolicyWarning = commandrequests.PolicyWarning
type CommandWorkspaceRuntimeDependencies = commandrequests.WorkspaceRuntimeDependencies
type ConnectorApprovalItem = connectorapproval.Item
type ConnectorApprovalNoteRequest = connectorapproval.NoteRequest
type ConnectorApprovalScope = connectorapproval.Scope
type ConnectorApprovalWorkflow = connectorapproval.Workflow
type Principal = executionprincipal.Principal
type MCPActionScope = mcpconnector.ActionScope
type MCPOutputAuthorization = mcpconnector.OutputAuthorization
type MCPPermission = mcpconnector.Permission
type MCPScope = mcpconnector.Scope
type Auth = runtimecontrol.Auth
type MCPRuntimeScope = runtimecontrol.MCPRuntimeScope
type SecurityHTTPScope = securitypolicy.HTTPScope
type SecuritySettings = securitypolicy.Settings
type Token = tokens.Token
type TokenValidationError = tokens.ValidationError
