package backups

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

type SnapshotSource struct {
	Database   *sql.DB
	DatabaseID string
	Path       string
}

type Snapshot struct {
	Path      string
	Filename  string
	CreatedAt time.Time
}

func CreateDatabaseSnapshot(ctx context.Context, source SnapshotSource) (Snapshot, error) {
	if source.Database == nil || strings.TrimSpace(source.DatabaseID) == "" || strings.TrimSpace(source.Path) == "" {
		return Snapshot{}, ErrIncompleteScope
	}
	info, err := os.Stat(source.Path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect database before snapshot: %w", err)
	}
	if info.Size() > MaxDatabaseTransferBytes {
		return Snapshot{}, fmt.Errorf("database is too large to snapshot through the gateway: %d bytes", info.Size())
	}
	createdAt := time.Now().UTC()
	snapshotPath, err := databasecatalog.ReserveTempPath(source.Path, "snapshot-"+source.DatabaseID+"-*.aipdb")
	if err != nil {
		return Snapshot{}, fmt.Errorf("reserve database snapshot path: %w", err)
	}
	if err := dbpkg.SnapshotContext(ctx, source.Database, snapshotPath); err != nil {
		return Snapshot{}, err
	}
	info, err = os.Stat(snapshotPath)
	if err != nil {
		_ = os.Remove(snapshotPath)
		return Snapshot{}, fmt.Errorf("inspect completed database snapshot: %w", err)
	}
	if info.Size() > MaxDatabaseTransferBytes {
		_ = os.Remove(snapshotPath)
		return Snapshot{}, fmt.Errorf("database snapshot exceeds the gateway limit: %d bytes", info.Size())
	}
	filename := strings.Trim(source.DatabaseID, "-")
	if filename == "" {
		filename = "aipermission"
	}
	return Snapshot{
		Path:      snapshotPath,
		Filename:  filename + "-" + createdAt.Format("20060102-150405") + ".aipdb",
		CreatedAt: createdAt,
	}, nil
}
