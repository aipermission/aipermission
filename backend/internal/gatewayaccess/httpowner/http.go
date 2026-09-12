// Package httpowner adapts gateway access contracts to their HTTP owners.
package httpowner

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
)

type VaultMetadataReaderFactory struct{}

func (VaultMetadataReaderFactory) ForDatabase(database *sql.DB) gatewayaccess.VaultMetadataReader {
	if database == nil {
		return nil
	}
	return vaultMetadataReader{database: database}
}

type vaultMetadataReader struct{ database *sql.DB }

func (reader vaultMetadataReader) CanRead(ctx context.Context, tokenID, projectID int64, now time.Time) (bool, error) {
	capability, err := accesscontrol.NewCapabilityStore(reader.database).Effective(
		ctx, tokenID, projectID, accesscontrol.VaultMetadataRead, now,
	)
	return err == nil && capability.ExecutionRule == accesscontrol.RuleAlwaysRun, err
}

type Factory struct{}

func (Factory) Build(providers gatewayaccess.ScopeProviders) gatewayaccess.HTTPHandlers {
	return gatewayaccess.HTTPHandlers{
		Security:            securitypolicy.NewHTTPHandlers(adaptSecurityScopeProvider(providers.Security)),
		TokenAccess:         accesscontrol.NewHTTPHandlers(adaptAccessScopeProvider(providers.TokenAccess)),
		MCPRuntime:          runtimecontrol.NewMCPHTTPHandlers(adaptMCPRuntimeScopeProvider(providers.MCPRuntime)),
		MCPConnectorReads:   mcpconnector.NewHTTPHandlers(adaptMCPReadScopeProvider(providers.MCPConnectorReads)),
		MCPConnectorActions: mcpconnector.NewActionHTTPHandlers(adaptMCPActionScopeProvider(providers.MCPConnectorActions)),
	}
}

func New(component *gatewayaccess.Component, providers gatewayaccess.ScopeProviders) gatewayaccess.HTTPHandlers {
	if component == nil {
		return gatewayaccess.HTTPHandlers{}
	}
	return (Factory{}).Build(providers)
}

func adaptSecurityScopeProvider(provider gatewayaccess.SecurityHTTPScopeProvider) securitypolicy.HTTPScopeProvider {
	if provider == nil {
		return nil
	}
	return func(w http.ResponseWriter) (securitypolicy.HTTPScope, bool) {
		scope, ok := provider(w)
		return securitypolicy.NewHTTPScope(scope.Service, scope.Mutate), ok
	}
}

func adaptAccessScopeProvider(provider gatewayaccess.AccessScopeProvider) accesscontrol.ScopeProvider {
	if provider == nil {
		return nil
	}
	return func(w http.ResponseWriter) (accesscontrol.Scope, bool) {
		scope, ok := provider(w)
		return accesscontrol.Scope{
			Database: scope.Database, Tokens: scope.Tokens, Registry: scope.Registry,
			ReusableTokens: scope.ReusableTokens, Mutate: accesscontrol.MutationRunner(scope.Mutate),
			AcquireExclusive: scope.AcquireExclusive, FinishTokenInvalidation: scope.FinishTokenInvalidation,
		}, ok
	}
}

func adaptMCPRuntimeScopeProvider(provider gatewayaccess.MCPRuntimeScopeProvider) runtimecontrol.MCPRuntimeScopeProvider {
	if provider == nil {
		return nil
	}
	return func(w http.ResponseWriter) (runtimecontrol.MCPRuntimeScope, bool) {
		scope, ok := provider(w)
		return runtimecontrol.MCPRuntimeScope{
			State: scope.State, StartEnabled: scope.StartEnabled, AcquireStop: scope.AcquireStop,
			StopEffects: scope.StopEffects, Observe: scope.Observe, Now: scope.Now,
		}, ok
	}
}

func adaptMCPReadScopeProvider(provider gatewayaccess.MCPScopeProvider) mcpconnector.ScopeProvider {
	if provider == nil {
		return nil
	}
	return func(w http.ResponseWriter, r *http.Request) (mcpconnector.Scope, bool) {
		scope, ok := provider(w, r)
		var permissions func(context.Context) ([]mcpconnector.Permission, error)
		if scope.Permissions != nil {
			permissions = func(ctx context.Context) ([]mcpconnector.Permission, error) {
				items, err := scope.Permissions(ctx)
				if err != nil {
					return nil, err
				}
				result := make([]mcpconnector.Permission, 0, len(items))
				for _, item := range items {
					result = append(result, mcpconnector.Permission{
						ProjectID: item.ProjectID, ProjectName: item.ProjectName, ProjectSlug: item.ProjectSlug,
						TargetID: item.TargetID, TargetName: item.TargetName, ProfileID: item.ProfileID,
						ProfileLabel: item.ProfileLabel, ConnectorKind: item.ConnectorKind, ProfileKind: item.ProfileKind,
						ActionName: item.ActionName, ExecutionRule: connectortargets.ActionPermissionRule(item.ExecutionRule), ExpiresAt: item.ExpiresAt,
					})
				}
				return result, nil
			}
		}
		var metadata func(connectors.TargetView, connectors.CredentialProfileView) map[string]any
		if scope.Metadata.Ready() {
			metadata = scope.Metadata.Resolve
		}
		return mcpconnector.Scope{
			Database: scope.Database, Registry: scope.Registry, TokenID: scope.TokenID,
			Permissions: permissions, MetadataEnabled: scope.MetadataEnabled, Metadata: metadata,
		}, ok
	}
}

func adaptMCPActionScopeProvider(provider gatewayaccess.MCPActionScopeProvider) mcpconnector.ActionScopeProvider {
	if provider == nil {
		return nil
	}
	return func(w http.ResponseWriter, r *http.Request) (mcpconnector.ActionScope, bool) {
		scope, ok := provider(w, r)
		var call func(context.Context, mcpconnector.ActionCall) (mcpconnector.ActionCallResult, error)
		if scope.Call != nil {
			call = func(ctx context.Context, request mcpconnector.ActionCall) (mcpconnector.ActionCallResult, error) {
				result, err := scope.Call(ctx, gatewayaccess.MCPActionCall{
					Source: request.Source, TokenID: request.TokenID, TargetRef: request.TargetRef,
					ActionName: request.ActionName, Input: request.Input, Reason: request.Reason,
					IdempotencyKey: request.IdempotencyKey,
				})
				return mcpconnector.ActionCallResult{Request: result.Request, Result: result.Result, Replayed: result.Replayed}, err
			}
		}
		return mcpconnector.ActionScope{
			Database: scope.Database, TokenID: scope.TokenID, Output: outputAuthorization(scope.Output),
			Call: call, Observe: scope.Observe, Redact: scope.Redact,
			RunningHint: func(request connectortargets.ActionRequest) string {
				if scope.RunningHint == nil {
					return ""
				}
				return scope.RunningHint(request)
			},
		}, ok
	}
}

func outputAuthorization(value *gatewayaccess.MCPOutputAuthorization) *mcpconnector.OutputAuthorization {
	if value == nil {
		return nil
	}
	return &mcpconnector.OutputAuthorization{
		Database: value.Database, Tokens: value.Tokens, Leases: value.Leases,
		Delivery: value.Delivery, MCPStarted: value.MCPStarted, Principal: value.Principal, Now: value.Now,
	}
}

func ResponseForToken(
	ctx context.Context,
	authorization *gatewayaccess.MCPOutputAuthorization,
	resolveRunningHint gatewayaccess.MCPRunningHint,
	tokenID int64,
	request connectortargets.ActionRequest,
	result connectors.ActionResult,
) actions.Response {
	return outputAuthorization(authorization).ResponseForToken(ctx, resolveRunningHint, tokenID, request, result)
}

func Authorized(ctx context.Context, authorization *gatewayaccess.MCPOutputAuthorization, tokenID int64, request connectortargets.ActionRequest) bool {
	return outputAuthorization(authorization).Authorized(ctx, tokenID, request)
}

func Deliver(
	w http.ResponseWriter,
	r *http.Request,
	authorization *gatewayaccess.MCPOutputAuthorization,
	tokenID int64,
	request connectortargets.ActionRequest,
	response actions.Response,
) {
	outputAuthorization(authorization).Deliver(w, r, tokenID, request, response)
}
