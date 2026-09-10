package api

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

func (s connectorTargetHandlers) decryptConnectorProfileSecrets(w http.ResponseWriter, runtime *databaseRuntime, profile connectortargets.CredentialProfile) (map[string]any, bool) {
	secrets := map[string]any{}
	if profile.EncryptedSecretJSON == "" {
		return secrets, true
	}
	if err := recordcrypto.DecryptJSON(runtime.Storage.Vault, runtime.WorkspaceUUID, recordcrypto.ConnectorCredentialProfile, profile.ID, profile.EncryptedSecretJSON, &secrets); err != nil {
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
