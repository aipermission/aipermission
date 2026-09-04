package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	glob      func(string) ([]string, error)
	mkdir     func(string, os.FileMode) error
	rename    func(string, string) error
	write     func(string, []byte, os.FileMode) error
	syncFile  func(string) error
	syncDir   func(string) error
	remove    func(string) error
	removeAll func(string) error
}

func defaultDatabaseMoveOps() databaseMoveOps {
	return databaseMoveOps{
		lstat: os.Lstat, glob: filepath.Glob, mkdir: os.Mkdir, rename: os.Rename,
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
	cleanupJournal := func() {
		if ops.removeAll(journalDir) == nil {
			_ = ops.syncDir(root)
		}
	}
	manifest := databaseMoveManifest{SourceBase: currentPath, TargetBase: targetPath, Moves: moves}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		cleanupJournal()
		return fmt.Errorf("encode database move journal: %w", err)
	}
	manifestPath := filepath.Join(journalDir, databaseMoveManifestFile)
	if err := ops.write(manifestPath, manifestJSON, 0o600); err != nil {
		cleanupJournal()
		return fmt.Errorf("write database move journal: %w", err)
	}
	if err := ops.syncFile(manifestPath); err != nil {
		cleanupJournal()
		return fmt.Errorf("sync database move manifest: %w", err)
	}
	if err := ops.syncDir(journalDir); err != nil {
		cleanupJournal()
		return fmt.Errorf("sync database move journal directory: %w", err)
	}
	if err := ops.syncDir(root); err != nil {
		cleanupJournal()
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
			cleanupJournal()
		}
		return errors.Join(errs...)
	}

	for _, item := range moves {
		if err := ops.rename(item.Source, item.Target); err != nil {
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
	cleanupJournal()
	return nil
}

func collectDatabaseMoves(currentPath, targetPath string, ops databaseMoveOps) ([]databaseMove, error) {
	candidates := []string{currentPath, currentPath + "-wal", currentPath + "-shm"}
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
		manifestJSON, err := os.ReadFile(filepath.Join(journalDir, databaseMoveManifestFile))
		if err != nil {
			return fmt.Errorf("read database move journal: %w", err)
		}
		var manifest databaseMoveManifest
		if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
			return fmt.Errorf("decode database move journal: %w", err)
		}
		if err := validateDatabaseMoveManifest(root, manifest); err != nil {
			return err
		}
		complete := Exists(filepath.Join(journalDir, databaseMoveCompleteFile))
		if err := recoverDatabaseMoveJournal(manifest, complete); err != nil {
			return err
		}
		if err := os.RemoveAll(journalDir); err != nil {
			return fmt.Errorf("remove recovered database move journal: %w", err)
		}
		if err := syncDatabaseDeletePath(root); err != nil {
			return fmt.Errorf("sync recovered database move root: %w", err)
		}
	}
	return nil
}

func recoverDatabaseMoveJournal(manifest databaseMoveManifest, complete bool) error {
	for index := len(manifest.Moves) - 1; index >= 0; index-- {
		item := manifest.Moves[index]
		sourceExists, targetExists := Exists(item.Source), Exists(item.Target)
		if complete {
			if sourceExists || !targetExists {
				return fmt.Errorf("completed database move journal has inconsistent artifact state")
			}
			continue
		}
		switch {
		case sourceExists && !targetExists:
			continue
		case !sourceExists && targetExists:
			if err := os.Rename(item.Target, item.Source); err != nil {
				return fmt.Errorf("recover database move source %q: %w", item.Source, err)
			}
		case sourceExists && targetExists:
			return fmt.Errorf("incomplete database move journal has duplicate artifact state")
		default:
			return fmt.Errorf("incomplete database move journal is missing an artifact")
		}
	}
	paths := make([]string, 0, len(manifest.Moves))
	if complete {
		for _, item := range manifest.Moves {
			paths = append(paths, item.Target)
		}
	} else {
		for _, item := range manifest.Moves {
			paths = append(paths, item.Source)
		}
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

func validateDatabaseMoveManifest(root string, manifest databaseMoveManifest) error {
	root, _ = filepath.Abs(filepath.Clean(root))
	sourceBase, sourceErr := filepath.Abs(filepath.Clean(manifest.SourceBase))
	targetBase, targetErr := filepath.Abs(filepath.Clean(manifest.TargetBase))
	if sourceErr != nil || targetErr != nil || sourceBase == targetBase || filepath.Ext(sourceBase) != ".db" || filepath.Ext(targetBase) != ".db" {
		return fmt.Errorf("database move journal has invalid base paths")
	}
	if !databaseMovePathWithin(root, sourceBase) || !databaseMovePathWithin(root, targetBase) || len(manifest.Moves) == 0 || len(manifest.Moves) > 64 {
		return fmt.Errorf("database move journal is outside its recovery root")
	}
	for _, item := range manifest.Moves {
		source, sourceErr := filepath.Abs(filepath.Clean(item.Source))
		target, targetErr := filepath.Abs(filepath.Clean(item.Target))
		suffix := strings.TrimPrefix(source, sourceBase)
		if sourceErr != nil || targetErr != nil || !validDatabaseMoveSuffix(suffix) || target != targetBase+suffix {
			return fmt.Errorf("database move journal has an invalid artifact path")
		}
	}
	return nil
}

func databaseMovePathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validDatabaseMoveSuffix(suffix string) bool {
	if suffix == "" || suffix == "-wal" || suffix == "-shm" {
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
