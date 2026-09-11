package api

import (
	"net/http"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s connectorTargetHandlers) decryptConnectorProfileSecrets(w http.ResponseWriter, runtime databaseRuntime, profile connectormgmt.CredentialProfile) (map[string]any, bool) {
	secrets := map[string]any{}
	if profile.EncryptedSecretJSON == "" {
		return secrets, true
	}
	if err := gatewayvault.DecryptJSON(runtime.StoragePort().SecretVault(), runtime.WorkspaceIdentifier(), gatewayvault.ConnectorCredentialProfileRecord(), profile.ID, profile.EncryptedSecretJSON, &secrets); err != nil {
		writeInternalError(w)
		return nil, false
	}
	return secrets, true
}

func handleConnectorProvisionError(w http.ResponseWriter, err error, safeMessage string) {
	if err == nil {
		return
	}
	status := http.StatusBadRequest
	if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		status = http.StatusConflict
	}
	writeErrorWithCode(w, status, safeMessage, connectors.ErrorCode(err))
}
