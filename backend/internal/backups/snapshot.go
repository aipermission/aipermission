package backups

import (
	"context"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/backups/snapshotfile"
)

type SnapshotSource = snapshotfile.Source
type Snapshot = snapshotfile.Result

func CreateDatabaseSnapshot(ctx context.Context, source SnapshotSource) (Snapshot, error) {
	if source.Database == nil || strings.TrimSpace(source.DatabaseID) == "" || strings.TrimSpace(source.Path) == "" {
		return Snapshot{}, ErrIncompleteScope
	}
	return snapshotfile.Create(ctx, source, MaxDatabaseTransferBytes)
}
