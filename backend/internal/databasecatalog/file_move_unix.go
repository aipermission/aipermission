//go:build linux || darwin

package databasecatalog

import (
	"fmt"
	"os"
	"path/filepath"
)

func syncFileForMove(path string) error {
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

func syncMoveDirectories(sourcePath string, targetPath string) error {
	directories := []string{filepath.Dir(targetPath)}
	if sourceDirectory := filepath.Dir(sourcePath); sourceDirectory != directories[0] {
		directories = append(directories, sourceDirectory)
	}
	for _, directoryPath := range directories {
		directory, err := os.Open(directoryPath)
		if err != nil {
			return fmt.Errorf("open moved file directory: %w", err)
		}
		if err := directory.Sync(); err != nil {
			_ = directory.Close()
			return fmt.Errorf("sync moved file directory: %w", err)
		}
		if err := directory.Close(); err != nil {
			return fmt.Errorf("close moved file directory: %w", err)
		}
	}
	return nil
}
