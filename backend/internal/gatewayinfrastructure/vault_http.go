package gatewayinfrastructure

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

type VaultMCPHTTPPorts struct {
	TokenID      int64
	MetadataRead func(context.Context, int64) (bool, error)
}

type VaultHTTPDependencies struct {
	Active       func(http.ResponseWriter) (*WorkspaceHandle, bool)
	RuntimePorts func(*WorkspaceHandle) VaultRuntimePorts
	ProjectPorts func(*WorkspaceHandle) ProjectPorts
	MCP          func(http.ResponseWriter, *http.Request) (*WorkspaceHandle, VaultMCPHTTPPorts, bool)
}

func (component *VaultOwner) HTTPHandlers(
	application *gatewayvault.Component,
	dependencies VaultHTTPDependencies,
) gatewayvault.HTTPHandlers {
	if component == nil || application == nil || dependencies.Active == nil ||
		dependencies.RuntimePorts == nil || dependencies.ProjectPorts == nil || dependencies.MCP == nil {
		return gatewayvault.HTTPHandlers{}
	}
	requestRuntime := func(handle *WorkspaceHandle) func(context.Context) (gatewayvault.VaultRequestApplication, error) {
		return func(ctx context.Context) (gatewayvault.VaultRequestApplication, error) {
			runtime, ok := component.vaultRuntime(handle, dependencies.RuntimePorts(handle))
			if !ok {
				return nil, ErrWorkspaceHandleUnavailable
			}
			return application.RequestRuntime(ctx, runtime)
		}
	}
	return application.HTTPHandlers(gatewayvault.HTTPDependencies{
		Projects: func(w http.ResponseWriter) (gatewayvault.ProjectScope, bool) {
			handle, ok := dependencies.Active(w)
			if !ok {
				return gatewayvault.ProjectScope{}, false
			}
			return component.projectScope(handle, dependencies.ProjectPorts(handle))
		},
		ProjectVault: func(w http.ResponseWriter) (gatewayvault.ProjectVaultHTTPScope, bool) {
			handle, ok := dependencies.Active(w)
			if !ok {
				return gatewayvault.ProjectVaultHTTPScope{}, false
			}
			runtime, ok := component.vaultRuntime(handle, dependencies.RuntimePorts(handle))
			if !ok {
				return gatewayvault.ProjectVaultHTTPScope{}, true
			}
			projectRuntime, err := application.ProjectRuntime(runtime)
			if err != nil {
				return gatewayvault.ProjectVaultHTTPScope{}, true
			}
			return gatewayvault.ProjectVaultHTTPScope{
				Runtime: projectRuntime, RuntimeID: handle.Identity().DatabaseID,
				SessionCatalog: application.SessionCatalog(runtime),
			}, true
		},
		VaultApprovals: func(w http.ResponseWriter) (gatewayvault.VaultApprovalHTTPScope, bool) {
			handle, ok := dependencies.Active(w)
			if !ok {
				return gatewayvault.VaultApprovalHTTPScope{}, false
			}
			return component.vaultApprovalScope(handle, requestRuntime(handle))
		},
		MCPVault: func(w http.ResponseWriter, r *http.Request) (gatewayvault.VaultMCPHTTPScope, bool) {
			handle, ports, ok := dependencies.MCP(w, r)
			if !ok {
				return gatewayvault.VaultMCPHTTPScope{}, false
			}
			return component.vaultMCPScope(handle, VaultMCPPorts{
				TokenID: ports.TokenID, Runtime: requestRuntime(handle), MetadataRead: ports.MetadataRead,
			})
		},
	})
}
