package backups

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
	"github.com/aipermission/aipermission/backend/internal/backups/providerlock"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

// ProviderSecretCodec keeps encrypted provider credentials behind the workspace-owned Vault boundary.
type ProviderSecretCodec interface {
	EncryptProviderSecret(int64, map[string]any) (string, error)
	DecryptProviderSecret(Provider) (map[string]any, error)
}

type DatabaseSnapshot struct {
	Path string
}

type OperationLease func(context.Context) (release func(), err error)
type Snapshotter func(context.Context) (DatabaseSnapshot, error)
type RequiredAudit func(context.Context, string, any) error
type ObservationAudit func(context.Context, string, any)
type DatabasePasswordAuthorizer func(http.ResponseWriter, *http.Request, string) bool

// HTTPScope contains only the active workspace capabilities needed by backup
// provider HTTP operations. Cross-domain database installation stays at the
// composition boundary and calls the exported restore preparation service.
type HTTPScope struct {
	Database             *sql.DB
	DatabaseID           string
	DatabaseName         string
	DatabasePath         string
	WorkspaceUUID        string
	InstallationDataPath string
	Secrets              ProviderSecretCodec
	Mutate               auditedmutation.Runner
	AuditRequired        RequiredAudit
	Observe              ObservationAudit
	CreateSnapshot       Snapshotter
	AuthorizePassword    DatabasePasswordAuthorizer
}

type HTTPScopeProvider func(http.ResponseWriter) (HTTPScope, bool)
type OperationHTTPScopeProvider func(http.ResponseWriter, *http.Request) (HTTPScope, func(), bool)

type HTTPHandlers struct {
	scope          HTTPScopeProvider
	operationScope OperationHTTPScopeProvider
	providerOps    providerlock.Manager
}

func NewHTTPHandlers(scope HTTPScopeProvider, operationScope OperationHTTPScopeProvider) *HTTPHandlers {
	return &HTTPHandlers{scope: scope, operationScope: operationScope}
}

func (h *HTTPHandlers) acquireProviderOperation(w http.ResponseWriter, ctx context.Context, database *sql.DB, providerID int64) (func(), bool) {
	release, err := h.providerOps.Acquire(ctx, database, providerID)
	if err != nil {
		httptransport.WriteError(w, http.StatusRequestTimeout, "backup provider operation was canceled")
		return nil, false
	}
	return release, true
}

type scopeRequirements uint16

const (
	MaxDatabaseTransferBytes int64             = 256 << 20
	requireDatabase          scopeRequirements = 1 << iota
	requireProviderIdentity
	requireDatabaseID
	requireDatabasePath
	requireInstallationPath
	requireSecrets
	requireMutation
	requireRequiredAudit
	requireObservation
	requireSnapshot
	requirePasswordAuthorization
)

func (h *HTTPHandlers) resolve(w http.ResponseWriter, requirements scopeRequirements) (HTTPScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return HTTPScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return HTTPScope{}, false
	}
	if !scopeSupports(scope, requirements) {
		httptransport.WriteInternalError(w)
		return HTTPScope{}, false
	}
	return scope, true
}

func scopeSupports(scope HTTPScope, requirements scopeRequirements) bool {
	valid := requirements&requireDatabase == 0 || scope.Database != nil
	valid = valid && (requirements&requireProviderIdentity == 0 || scope.DatabaseName != "" && scope.WorkspaceUUID != "")
	valid = valid && (requirements&requireDatabaseID == 0 || scope.DatabaseID != "")
	valid = valid && (requirements&requireDatabasePath == 0 || scope.DatabasePath != "")
	valid = valid && (requirements&requireInstallationPath == 0 || scope.InstallationDataPath != "")
	valid = valid && (requirements&requireSecrets == 0 || scope.Secrets != nil)
	valid = valid && (requirements&requireMutation == 0 || scope.Mutate != nil)
	valid = valid && (requirements&requireRequiredAudit == 0 || scope.AuditRequired != nil)
	valid = valid && (requirements&requireObservation == 0 || scope.Observe != nil)
	valid = valid && (requirements&requireSnapshot == 0 || scope.CreateSnapshot != nil)
	return valid && (requirements&requirePasswordAuthorization == 0 || scope.AuthorizePassword != nil)
}

func (h *HTTPHandlers) resolveOperation(w http.ResponseWriter, r *http.Request, requirements scopeRequirements) (HTTPScope, func(), bool) {
	if h == nil || h.operationScope == nil {
		httptransport.WriteInternalError(w)
		return HTTPScope{}, nil, false
	}
	scope, release, ok := h.operationScope(w, r)
	if !ok {
		return HTTPScope{}, nil, false
	}
	if release == nil || !scopeSupports(scope, requirements) {
		if release != nil {
			release()
		}
		httptransport.WriteInternalError(w)
		return HTTPScope{}, nil, false
	}
	return scope, release, true
}
