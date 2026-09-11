package gatewayvault

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/applicationvault"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func NewApplication(projects applicationvault.ProjectDependencies) *applicationvault.Component {
	return applicationvault.New(projects)
}

func NewProjectsHTTPHandlers(scope projects.ScopeProvider) *projects.HTTPHandlers {
	return projects.NewHTTPHandlers(scope)
}

func WriteProjectHTTPError(w http.ResponseWriter, err error) {
	projects.WriteHTTPError(w, err)
}

func EnsureWorkspaceUUID(ctx context.Context, db *sql.DB) (string, error) {
	return projectvault.EnsureWorkspaceUUID(ctx, db)
}

func NewProjectVaultHTTPHandlers(scope projectvault.HTTPScopeProvider) *projectvault.HTTPHandlers {
	return projectvault.NewHTTPHandlers(scope)
}

func DecryptJSON(secretVault recordcrypto.JSONDecrypter, workspaceID string, recordType recordcrypto.RecordType, recordID int64, encrypted string, target any) error {
	return recordcrypto.DecryptJSON(secretVault, workspaceID, recordType, recordID, encrypted, target)
}

func ConnectorCredentialProfileRecord() recordcrypto.RecordType {
	return recordcrypto.ConnectorCredentialProfile
}

func IsUIExempt(path string) bool {
	return uisession.IsExempt(path)
}

func PrepareUISession() (uisession.Prepared, error) {
	return uisession.Prepare()
}

func UISessionRetryIdentity(instanceID string) string {
	return uisession.RetryIdentity(instanceID)
}

func NewVaultApprovalHTTPHandlers(scope vaultrequests.HTTPScopeProvider) *vaultrequests.HTTPHandlers {
	return vaultrequests.NewHTTPHandlers(scope)
}

func NewVaultMCPHTTPHandlers(scope vaultrequests.MCPHTTPScopeProvider) *vaultrequests.MCPHTTPHandlers {
	return vaultrequests.NewMCPHTTPHandlers(scope)
}

func NewInvalidator(dependencies vaultsessions.InvalidatorDependencies) (*vaultsessions.Invalidator, error) {
	return vaultsessions.NewInvalidator(dependencies)
}

func NewPersistence(database vaultsessions.Executor) *vaultsessions.Persistence {
	return vaultsessions.NewPersistence(database)
}
