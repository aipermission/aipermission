//go:build !windows

package databasecatalog

import "os"

func syncDatabaseDeletePath(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
