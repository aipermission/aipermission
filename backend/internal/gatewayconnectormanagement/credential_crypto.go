package gatewayconnectormanagement

import "github.com/aipermission/aipermission/backend/internal/recordcrypto"

func DecryptCredentialProfileJSON(secretVault recordcrypto.JSONDecrypter, workspaceID string, profileID int64, encrypted string, target any) error {
	return recordcrypto.DecryptJSON(secretVault, workspaceID, recordcrypto.ConnectorCredentialProfile, profileID, encrypted, target)
}
