package backups

import (
	"context"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/backups/snapshotfile"
)

func (client *ServiceClient) downloadVerifiedTemp(ctx context.Context, databasePath, pattern, streamID string, version ServiceBackup) (string, error) {
	return snapshotfile.Stage(databasePath, pattern, func(path string) (bool, error) {
		downloaded, err := client.Download(ctx, streamID, version.ID, path, MaxDatabaseTransferBytes)
		if err != nil {
			return false, err
		}
		if downloaded.SizeBytes != version.SizeBytes || !strings.EqualFold(downloaded.SHA256, version.SHA256) {
			return true, ErrTransientBackupChanged
		}
		return true, nil
	})
}
