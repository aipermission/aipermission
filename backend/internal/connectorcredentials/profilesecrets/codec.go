package profilesecrets

import (
	"context"
	"errors"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

// ProfileSecretCodec belongs to core credential composition. Its fixed record
// class prevents callers from selecting another domain or encrypted field.
type ProfileSecretCodec struct {
	vault       *vault.Vault
	workspaceID string
}

func NewProfileSecretCodec(secretVault *vault.Vault, workspaceID string) ProfileSecretCodec {
	return ProfileSecretCodec{vault: secretVault, workspaceID: workspaceID}
}

func (codec ProfileSecretCodec) Available() bool {
	return codec.vault != nil && strings.TrimSpace(codec.workspaceID) != ""
}

func (codec ProfileSecretCodec) Encrypt(_ context.Context, profileID int64, secret map[string]any) (string, error) {
	if !codec.Available() {
		return "", errors.New("credential profile secret storage is unavailable")
	}
	return recordcrypto.EncryptJSON(codec.vault, codec.workspaceID, recordcrypto.ConnectorCredentialProfile, profileID, secret)
}

func (codec ProfileSecretCodec) Decrypt(_ context.Context, profileID int64, encrypted string) (map[string]any, error) {
	if !codec.Available() {
		return nil, errors.New("credential profile secret storage is unavailable")
	}
	secret := map[string]any{}
	if err := recordcrypto.DecryptJSON(codec.vault, codec.workspaceID, recordcrypto.ConnectorCredentialProfile, profileID, encrypted, &secret); err != nil {
		return nil, err
	}
	return secret, nil
}
