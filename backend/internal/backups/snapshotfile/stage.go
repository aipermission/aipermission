package snapshotfile

import (
	"os"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
)

// Stage reserves an unused artifact name. Writers report ownership explicitly:
// an exclusive-create failure must never remove somebody else's existing file.
func Stage(databasePath, pattern string, write func(string) (owned bool, err error)) (string, error) {
	path, err := databasecatalog.ReserveTempPath(databasePath, pattern)
	if err != nil {
		return "", err
	}
	if owned, err := write(path); err != nil {
		if owned {
			_ = os.Remove(path)
		}
		return "", err
	}
	return path, nil
}
