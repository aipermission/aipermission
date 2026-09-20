package databasecatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/databaseownership"
)

const (
	databaseMoveJournalPrefix = ".aipermission-move-"
	databaseMoveManifestFile  = "manifest.json"
	databaseMoveCompleteFile  = ".complete"
)

type databaseMove struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type databaseMoveManifest struct {
	SourceBase string         `json:"source_base"`
	TargetBase string         `json:"target_base"`
	Moves      []databaseMove `json:"moves"`
}

type databaseMoveOps struct {
	lstat     func(string) (os.FileInfo, error)
	readDir   func(string) ([]os.DirEntry, error)
	glob      func(string) ([]string, error)
	mkdir     func(string, os.FileMode) error
	rename    func(string, string) error
	publish   func(string, string) error
	write     func(string, []byte, os.FileMode) error
	syncFile  func(string) error
	syncDir   func(string) error
	remove    func(string) error
	removeAll func(string) error
}

func defaultDatabaseMoveOps() databaseMoveOps {
	return databaseMoveOps{
		lstat: os.Lstat, readDir: os.ReadDir, glob: filepath.Glob, mkdir: os.Mkdir, rename: moveFileNoReplace, publish: moveFileNoReplace,
		write: os.WriteFile, syncFile: syncDatabaseDeletePath,
		syncDir: syncDatabaseDeletePath, remove: os.Remove, removeAll: os.RemoveAll,
	}
}

func moveDatabaseWithOps(currentPath string, targetPath string, ops databaseMoveOps) error {
	var err error
	currentPath, err = filepath.Abs(filepath.Clean(currentPath))
	if err != nil {
		return fmt.Errorf("resolve database move source: %w", err)
	}
	targetPath, err = filepath.Abs(filepath.Clean(targetPath))
	if err != nil {
		return fmt.Errorf("resolve database move target: %w", err)
	}
	moves, err := collectDatabaseMoves(currentPath, targetPath, ops)
	if err != nil {
		return err
	}
	root := databaseMoveRoot(currentPath, targetPath)
	suffix, err := databaseDeleteQuarantineSuffix()
	if err != nil {
		return fmt.Errorf("create database move journal id: %w", err)
	}
	journalDir := filepath.Join(root, databaseMoveJournalPrefix+suffix)
	if err := ops.mkdir(journalDir, 0o700); err != nil {
		return fmt.Errorf("create database move journal: %w", err)
	}
	cleanupIncompleteJournal := func() {
		if ops.removeAll(journalDir) == nil {
			_ = ops.syncDir(root)
		}
	}
	manifest := databaseMoveManifest{SourceBase: currentPath, TargetBase: targetPath, Moves: moves}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		cleanupIncompleteJournal()
		return fmt.Errorf("encode database move journal: %w", err)
	}
	manifestPath := filepath.Join(journalDir, databaseMoveManifestFile)
	pendingManifestPath := manifestPath + ".pending"
	if err := ops.write(pendingManifestPath, manifestJSON, 0o600); err != nil {
		cleanupIncompleteJournal()
		return fmt.Errorf("write database move journal: %w", err)
	}
	if err := ops.syncFile(pendingManifestPath); err != nil {
		cleanupIncompleteJournal()
		return fmt.Errorf("sync database move manifest: %w", err)
	}
	if err := ops.publish(pendingManifestPath, manifestPath); err != nil {
		cleanupIncompleteJournal()
		return fmt.Errorf("publish database move manifest: %w", err)
	}
	if err := ops.syncDir(journalDir); err != nil {
		cleanupIncompleteJournal()
		return fmt.Errorf("sync database move journal directory: %w", err)
	}
	if err := ops.syncDir(root); err != nil {
		cleanupIncompleteJournal()
		return fmt.Errorf("sync database move root: %w", err)
	}

	moved := make([]databaseMove, 0, len(moves))
	rollback := func(cause error) error {
		errs := []error{cause}
		markerPath := filepath.Join(journalDir, databaseMoveCompleteFile)
		if err := ops.remove(markerPath); err != nil && !os.IsNotExist(err) {
			// The marker may already be durable. Preserve the artifact state and
			// let startup recovery decide whether the target is authoritative.
			return errors.Join(cause, fmt.Errorf("remove database move completion marker: %w", err))
		}
		for index := len(moved) - 1; index >= 0; index-- {
			item := moved[index]
			if err := ops.rename(item.Target, item.Source); err != nil {
				errs = append(errs, fmt.Errorf("restore database move source %q: %w", item.Source, err))
			}
		}
		for _, item := range moved {
			if _, err := ops.lstat(item.Source); err == nil {
				if err := ops.syncFile(item.Source); err != nil {
					errs = append(errs, fmt.Errorf("sync restored database move source %q: %w", item.Source, err))
				}
			}
		}
		for _, dir := range uniqueDatabaseDirs(currentPath, targetPath, journalDir) {
			if err := ops.syncDir(dir); err != nil {
				errs = append(errs, fmt.Errorf("sync database move rollback directory %q: %w", dir, err))
			}
		}
		if len(errs) == 1 {
			cleanupIncompleteJournal()
		}
		return errors.Join(errs...)
	}

	for _, item := range moves {
		if err := ops.rename(item.Source, item.Target); err != nil {
			if errors.Is(err, errDurableFileMoveStateIndeterminate) {
				// The platform move crossed the rename boundary but could not
				// prove its own rollback. Include this artifact in the outer
				// rollback so the journal is only removed after a known-good
				// source state has been restored.
				moved = append(moved, item)
			}
			return rollback(fmt.Errorf("rename database artifact %q: %w", item.Source, err))
		}
		moved = append(moved, item)
	}
	for _, item := range moved {
		if err := ops.syncFile(item.Target); err != nil {
			return rollback(fmt.Errorf("sync renamed database artifact %q: %w", item.Target, err))
		}
	}
	for _, dir := range uniqueDatabaseDirs(currentPath, targetPath) {
		if err := ops.syncDir(dir); err != nil {
			return rollback(fmt.Errorf("sync database move directory %q: %w", dir, err))
		}
	}
	markerPath := filepath.Join(journalDir, databaseMoveCompleteFile)
	if err := ops.write(markerPath, []byte("complete\n"), 0o600); err != nil {
		return rollback(fmt.Errorf("complete database move journal: %w", err))
	}
	if err := ops.syncFile(markerPath); err != nil {
		return rollback(fmt.Errorf("sync database move completion marker: %w", err))
	}
	if err := ops.syncDir(journalDir); err != nil {
		return rollback(fmt.Errorf("sync completed database move journal: %w", err))
	}
	if err := ops.syncDir(root); err != nil {
		return rollback(fmt.Errorf("sync completed database move root: %w", err))
	}

	// A durable completion marker makes the target authoritative. Journal cleanup
	// is best effort and is retried by the database catalog on startup.
	_ = removeCompletedMoveJournalWithOps(root, journalDir, ops)
	return nil
}

func collectDatabaseMoves(currentPath, targetPath string, ops databaseMoveOps) ([]databaseMove, error) {
	candidates := []string{currentPath, currentPath + "-wal", currentPath + "-shm", currentPath + "-journal"}
	for _, pattern := range []string{currentPath + ".pre-migration-v*.aipdb", currentPath + ".pre-migration-v*.aipdb.pending"} {
		matches, err := ops.glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("inspect database recovery artifacts: %w", err)
		}
		candidates = append(candidates, matches...)
	}
	moves := make([]databaseMove, 0, len(candidates))
	for _, source := range candidates {
		if _, err := ops.lstat(source); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("inspect database move source %q: %w", source, err)
		}
		target := targetPath + strings.TrimPrefix(source, currentPath)
		if _, err := ops.lstat(target); err == nil {
			return nil, fmt.Errorf("database move target already exists: %s", target)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect database move target %q: %w", target, err)
		}
		moves = append(moves, databaseMove{Source: source, Target: target})
	}
	if len(moves) == 0 || moves[0].Source != currentPath {
		return nil, fmt.Errorf("rename database: source does not exist")
	}
	return moves, nil
}

func recoverDatabaseMoveJournals(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect database move journals: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), databaseMoveJournalPrefix) {
			continue
		}
		journalDir := filepath.Join(root, entry.Name())
		complete, err := databaseMoveJournalComplete(journalDir)
		if err != nil {
			return err
		}
		if complete {
			if err := removeCompletedMoveJournal(root, journalDir); err != nil {
				return err
			}
			continue
		}
		manifestPath := filepath.Join(journalDir, databaseMoveManifestFile)
		manifestJSON, err := readRegularDatabaseMoveFile(manifestPath, "manifest")
		if err != nil {
			if os.IsNotExist(err) && moveJournalIsUnpublished(journalDir) {
				if cleanupErr := removeIncompleteMoveJournal(root, journalDir); cleanupErr != nil {
					return cleanupErr
				}
				continue
			}
			return fmt.Errorf("read database move journal: %w", err)
		}
		var manifest databaseMoveManifest
		if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
			return fmt.Errorf("decode database move journal: %w", err)
		}
		if err := validateDatabaseMoveManifest(root, manifest); err != nil {
			return err
		}
		ownerships, err := acquireDatabaseOwnershipSet(manifest.SourceBase, manifest.TargetBase)
		if errors.Is(err, databaseownership.ErrDatabaseInUse) {
			continue
		}
		if err != nil {
			return fmt.Errorf("claim incomplete database move journal: %w", err)
		}
		if err := recoverDatabaseMoveJournal(manifest); err != nil {
			closeDatabaseOwnershipSet(ownerships)
			return err
		}
		if err := os.RemoveAll(journalDir); err != nil {
			closeDatabaseOwnershipSet(ownerships)
			return fmt.Errorf("remove recovered database move journal: %w", err)
		}
		if err := syncDatabaseDeletePath(root); err != nil {
			closeDatabaseOwnershipSet(ownerships)
			return fmt.Errorf("sync recovered database move root: %w", err)
		}
		closeDatabaseOwnershipSet(ownerships)
	}
	return nil
}

func readRegularDatabaseMoveFile(path, label string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("database move %s is not a regular file", label)
	}
	return os.ReadFile(path)
}

func databaseMoveJournalComplete(journalDir string) (bool, error) {
	markerPath := filepath.Join(journalDir, databaseMoveCompleteFile)
	marker, err := readRegularDatabaseMoveFile(markerPath, "completion marker")
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read database move completion marker: %w", err)
	}
	if string(marker) != "complete\n" {
		return false, errors.New("database move journal has an invalid completion marker")
	}
	return true, nil
}

func removeCompletedMoveJournal(root, journalDir string) error {
	return removeCompletedMoveJournalWithOps(root, journalDir, defaultDatabaseMoveOps())
}

func removeCompletedMoveJournalWithOps(root, journalDir string, ops databaseMoveOps) error {
	entries, err := ops.readDir(journalDir)
	if err != nil {
		return fmt.Errorf("inspect completed database move journal: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == databaseMoveCompleteFile {
			continue
		}
		if err := ops.remove(filepath.Join(journalDir, entry.Name())); err != nil {
			return fmt.Errorf("remove completed database move journal artifact %q: %w", entry.Name(), err)
		}
	}
	if err := ops.syncDir(journalDir); err != nil {
		return fmt.Errorf("sync completed database move journal artifacts: %w", err)
	}
	if err := ops.remove(filepath.Join(journalDir, databaseMoveCompleteFile)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove completed database move marker: %w", err)
	}
	if err := ops.syncDir(journalDir); err != nil {
		return fmt.Errorf("sync completed database move marker removal: %w", err)
	}
	if err := ops.remove(journalDir); err != nil {
		return fmt.Errorf("remove completed database move journal: %w", err)
	}
	if err := ops.syncDir(root); err != nil {
		return fmt.Errorf("sync completed database move journal removal: %w", err)
	}
	return nil
}

func moveJournalIsUnpublished(journalDir string) bool {
	if _, err := os.Lstat(filepath.Join(journalDir, databaseMoveManifestFile+".pending")); err == nil {
		return true
	}
	entries, err := os.ReadDir(journalDir)
	return err == nil && len(entries) == 0
}

func removeIncompleteMoveJournal(root, journalDir string) error {
	if err := os.RemoveAll(journalDir); err != nil {
		return fmt.Errorf("remove incomplete database move journal: %w", err)
	}
	if err := syncDatabaseDeletePath(root); err != nil {
		return fmt.Errorf("sync incomplete database move journal removal: %w", err)
	}
	return nil
}

func recoverDatabaseMoveJournal(manifest databaseMoveManifest) error {
	return recoverDatabaseMoveJournalWithPublish(manifest, moveFileNoReplace)
}

func recoverDatabaseMoveJournalWithPublish(manifest databaseMoveManifest, publish func(string, string) error) error {
	restore := make([]databaseMove, 0, len(manifest.Moves))
	duplicateLinks := make([]databaseMove, 0, len(manifest.Moves))
	for index := len(manifest.Moves) - 1; index >= 0; index-- {
		item := manifest.Moves[index]
		sourceExists, sourceErr := databaseMoveRegularFileExists(item.Source)
		targetExists, targetErr := databaseMoveRegularFileExists(item.Target)
		if sourceErr != nil || targetErr != nil {
			return errors.Join(sourceErr, targetErr)
		}
		switch {
		case sourceExists && !targetExists:
			continue
		case !sourceExists && targetExists:
			restore = append(restore, item)
		case sourceExists && targetExists:
			same, err := databaseArtifactsAreSameFile(item.Source, item.Target)
			if err != nil {
				return err
			}
			if !same {
				return fmt.Errorf("incomplete database move journal has duplicate artifact state")
			}
			duplicateLinks = append(duplicateLinks, item)
		default:
			return fmt.Errorf("incomplete database move journal is missing an artifact")
		}
	}
	restored := make([]databaseMove, 0, len(restore))
	for _, item := range restore {
		if err := publish(item.Target, item.Source); err != nil {
			recoveryErrors := []error{fmt.Errorf("recover database move source %q: %w", item.Source, err)}
			for index := len(restored) - 1; index >= 0; index-- {
				restoredItem := restored[index]
				if rollbackErr := publish(restoredItem.Source, restoredItem.Target); rollbackErr != nil {
					recoveryErrors = append(recoveryErrors, fmt.Errorf("roll back recovered database move source %q: %w", restoredItem.Source, rollbackErr))
				}
			}
			return errors.Join(recoveryErrors...)
		}
		restored = append(restored, item)
	}
	for _, item := range duplicateLinks {
		same, err := databaseArtifactsAreSameFile(item.Source, item.Target)
		if err != nil {
			return err
		}
		if !same {
			return fmt.Errorf("incomplete database move journal artifact identity changed")
		}
		if err := os.Remove(item.Target); err != nil {
			return fmt.Errorf("remove duplicate database move target %q: %w", item.Target, err)
		}
	}
	paths := make([]string, 0, len(manifest.Moves))
	for _, item := range manifest.Moves {
		paths = append(paths, item.Source)
	}
	for _, path := range paths {
		if err := syncDatabaseDeletePath(path); err != nil {
			return fmt.Errorf("sync recovered database move artifact %q: %w", path, err)
		}
	}
	for _, dir := range uniqueDatabaseDirs(manifest.SourceBase, manifest.TargetBase) {
		if err := syncDatabaseDeletePath(dir); err != nil {
			return fmt.Errorf("sync recovered database move directory %q: %w", dir, err)
		}
	}
	return nil
}

func databaseArtifactsAreSameFile(firstPath, secondPath string) (bool, error) {
	first, err := os.Stat(firstPath)
	if err != nil {
		return false, fmt.Errorf("inspect database artifact %q: %w", firstPath, err)
	}
	second, err := os.Stat(secondPath)
	if err != nil {
		return false, fmt.Errorf("inspect database artifact %q: %w", secondPath, err)
	}
	return os.SameFile(first, second), nil
}

func validateDatabaseMoveManifest(root string, manifest databaseMoveManifest) error {
	root, _ = filepath.Abs(filepath.Clean(root))
	sourceBase, sourceErr := filepath.Abs(filepath.Clean(manifest.SourceBase))
	targetBase, targetErr := filepath.Abs(filepath.Clean(manifest.TargetBase))
	if sourceErr != nil || targetErr != nil || sourceBase != manifest.SourceBase || targetBase != manifest.TargetBase || sourceBase == targetBase || filepath.Ext(sourceBase) != ".db" || filepath.Ext(targetBase) != ".db" {
		return fmt.Errorf("database move journal has invalid base paths")
	}
	if !databaseMovePathWithin(root, sourceBase) || !databaseMovePathWithin(root, targetBase) || len(manifest.Moves) == 0 || len(manifest.Moves) > 64 {
		return fmt.Errorf("database move journal is outside its recovery root")
	}
	if !validDatabaseMoveBaseDirectory(root, filepath.Dir(sourceBase)) || !validDatabaseMoveBaseDirectory(root, filepath.Dir(targetBase)) {
		return fmt.Errorf("database move journal uses an invalid catalog directory")
	}
	if _, err := checkedDatabasePath(sourceBase); err != nil {
		return fmt.Errorf("database move journal source path: %w", err)
	}
	if _, err := checkedDatabasePath(targetBase); err != nil {
		return fmt.Errorf("database move journal target path: %w", err)
	}
	for _, item := range manifest.Moves {
		source, sourceErr := filepath.Abs(filepath.Clean(item.Source))
		target, targetErr := filepath.Abs(filepath.Clean(item.Target))
		suffix := strings.TrimPrefix(source, sourceBase)
		if sourceErr != nil || targetErr != nil || source != item.Source || target != item.Target || !validDatabaseMoveSuffix(suffix) || target != targetBase+suffix {
			return fmt.Errorf("database move journal has an invalid artifact path")
		}
		if _, err := checkedDatabasePath(source); err != nil {
			return fmt.Errorf("database move journal source artifact: %w", err)
		}
		if _, err := checkedDatabasePath(target); err != nil {
			return fmt.Errorf("database move journal target artifact: %w", err)
		}
	}
	return nil
}

func validDatabaseMoveBaseDirectory(root, directory string) bool {
	return directory == root || directory == filepath.Join(root, "databases")
}

func databaseMoveRegularFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect database move artifact %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("database move artifact %q is not a regular file", path)
	}
	return true, nil
}

func databaseMovePathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validDatabaseMoveSuffix(suffix string) bool {
	if suffix == "" || suffix == "-wal" || suffix == "-shm" || suffix == "-journal" {
		return true
	}
	return strings.HasPrefix(suffix, ".pre-migration-v") &&
		(strings.HasSuffix(suffix, ".aipdb") || strings.HasSuffix(suffix, ".aipdb.pending"))
}

func databaseMoveRoot(paths ...string) string {
	if len(paths) == 0 {
		return "."
	}
	root, _ := filepath.Abs(filepath.Dir(paths[0]))
	for _, path := range paths[1:] {
		candidate, _ := filepath.Abs(filepath.Dir(path))
		for !databaseMovePathWithin(root, candidate) {
			parent := filepath.Dir(root)
			if parent == root {
				return root
			}
			root = parent
		}
	}
	return root
}

func uniqueDatabaseDirs(paths ...string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		dir := filepath.Dir(path)
		if !seen[dir] {
			seen[dir] = true
			result = append(result, dir)
		}
	}
	return result
}
