package backups

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const MaxDatabaseTransferBytes int64 = 256 << 20

// ProviderSecretCodec keeps encrypted provider credentials behind the
// workspace-owned Vault boundary.
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
	AcquireOperation     OperationLease
	CreateSnapshot       Snapshotter
}

type HTTPScopeProvider func(http.ResponseWriter) (HTTPScope, bool)

type HTTPHandlers struct{ scope HTTPScopeProvider }

func NewHTTPHandlers(scope HTTPScopeProvider) *HTTPHandlers {
	return &HTTPHandlers{scope: scope}
}

type scopeRequirements uint16

const (
	requireDatabase scopeRequirements = 1 << iota
	requireProviderIdentity
	requireDatabaseID
	requireDatabasePath
	requireInstallationPath
	requireSecrets
	requireMutation
	requireRequiredAudit
	requireObservation
	requireOperation
	requireSnapshot
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
	valid = valid && (requirements&requireOperation == 0 || scope.AcquireOperation != nil)
	return valid && (requirements&requireSnapshot == 0 || scope.CreateSnapshot != nil)
}
