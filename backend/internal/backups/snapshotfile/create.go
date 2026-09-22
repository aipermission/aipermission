package snapshotfile

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

type Source struct {
	Database   *sql.DB
	DatabaseID string
	Path       string
}

type Result struct {
	Path      string
	Filename  string
	CreatedAt time.Time
}

func Create(ctx context.Context, source Source, maxBytes int64) (Result, error) {
	info, err := os.Stat(source.Path)
	if err != nil {
		return Result{}, fmt.Errorf("inspect database before snapshot: %w", err)
	}
	if info.Size() > maxBytes {
		return Result{}, fmt.Errorf("database is too large to snapshot through the gateway: %d bytes", info.Size())
	}
	createdAt := time.Now().UTC()
	snapshotPath, err := databasecatalog.ReserveTempPath(source.Path, "snapshot-"+source.DatabaseID+"-*.aipdb")
	if err != nil {
		return Result{}, fmt.Errorf("reserve database snapshot path: %w", err)
	}
	if err := dbpkg.SnapshotContext(ctx, source.Database, snapshotPath); err != nil {
		return Result{}, err
	}
	info, err = os.Stat(snapshotPath)
	if err != nil {
		_ = os.Remove(snapshotPath)
		return Result{}, fmt.Errorf("inspect completed database snapshot: %w", err)
	}
	if info.Size() > maxBytes {
		_ = os.Remove(snapshotPath)
		return Result{}, fmt.Errorf("database snapshot exceeds the gateway limit: %d bytes", info.Size())
	}
	filename := strings.Trim(source.DatabaseID, "-")
	if filename == "" {
		filename = "aipermission"
	}
	return Result{
		Path:      snapshotPath,
		Filename:  filename + "-" + createdAt.Format("20060102-150405") + ".aipdb",
		CreatedAt: createdAt,
	}, nil
}
