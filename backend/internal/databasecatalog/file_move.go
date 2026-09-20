package databasecatalog

import (
	"errors"
	"fmt"
)

var errDurableFileMoveStateIndeterminate = errors.New("durable file move state is indeterminate")

// moveFileNoReplace atomically moves a catalog artifact without relying on hard-link support.
func moveFileNoReplace(sourcePath string, targetPath string) error {
	if err := syncFileForMove(sourcePath); err != nil {
		return fmt.Errorf("sync file for durable move: %w", err)
	}
	if err := renameFileNoReplace(sourcePath, targetPath); err != nil {
		return err
	}
	if err := syncMoveDirectories(sourcePath, targetPath); err != nil {
		rollbackErr := renameFileNoReplace(targetPath, sourcePath)
		if rollbackErr == nil {
			rollbackErr = syncMoveDirectories(targetPath, sourcePath)
		}
		if rollbackErr != nil {
			return errors.Join(
				errDurableFileMoveStateIndeterminate,
				err,
				fmt.Errorf("roll back durable file move: %w", rollbackErr),
			)
		}
		return err
	}
	return nil
}
