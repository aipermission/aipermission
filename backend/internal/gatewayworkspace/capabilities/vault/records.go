package vault

import "github.com/aipermission/aipermission/backend/internal/recordcrypto"

func (capability RuntimeCapability) SealVaultActionRequest(workspaceID string, id int64, value any) (string, error) {
	return recordcrypto.EncryptJSON(capability.Vault, workspaceID, recordcrypto.VaultActionRequest, id, value)
}

func (capability RuntimeCapability) OpenVaultActionRequest(workspaceID string, id int64, sealed string, target any) error {
	return recordcrypto.DecryptJSON(capability.Vault, workspaceID, recordcrypto.VaultActionRequest, id, sealed, target)
}
