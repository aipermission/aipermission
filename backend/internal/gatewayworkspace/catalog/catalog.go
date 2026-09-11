package catalog

import (
	"time"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/db"
)

func Scavenge(path string, now time.Time) { databasecatalog.ScavengeTempPaths(path, now) }
func DefaultID(path string) string        { return databasecatalog.DefaultDatabaseID(path) }
func Move(currentPath, targetPath string) error {
	return databasecatalog.MoveDatabase(currentPath, targetPath)
}
func Delete(path string) error { return databasecatalog.DeleteDatabase(path) }
func Publish(sourcePath, targetPath string) error {
	return db.PublishFileNoReplace(sourcePath, targetPath)
}
func LooksPlaintext(path string) bool { return db.LooksLikePlainSQLite(path) }
