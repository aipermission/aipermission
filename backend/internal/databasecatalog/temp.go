package databasecatalog

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const databaseTempDirectoryName = ".aipermission-temp"

func ReserveTempPath(databasePath, pattern string) (string, error) {
	directory := filepath.Join(filepath.Dir(databasePath), databaseTempDirectoryName)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create database temporary directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", fmt.Errorf("reserve database temporary path: %w", err)
	}
	path := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close reserved database temporary path: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("prepare database temporary path: %w", err)
	}
	return path, nil
}

func ScavengeTempPaths(defaultPath string, now time.Time) {
	type scavengerDirectory struct {
		path    string
		matches func(string) bool
	}
	root := filepath.Dir(defaultPath)
	directories := []scavengerDirectory{
		{path: root, matches: isLegacyDatabaseTemporaryFile},
		{path: filepath.Join(root, "databases"), matches: isLegacyDatabaseTemporaryFile},
		{path: filepath.Join(root, databaseTempDirectoryName), matches: isDatabaseTemporaryFile},
		{path: filepath.Join(root, "databases", databaseTempDirectoryName), matches: isDatabaseTemporaryFile},
	}
	for _, directory := range directories {
		entries, err := os.ReadDir(directory.path)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("inspect stale database temporary files path=%q error=%v", directory.path, err)
			}
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !directory.matches(entry.Name()) {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || now.Sub(info.ModTime()) < 24*time.Hour {
				continue
			}
			path := filepath.Join(directory.path, entry.Name())
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				log.Printf("remove stale database temporary file path=%q error=%v", path, err)
			}
		}
	}
}

func isDatabaseTemporaryFile(name string) bool {
	return strings.HasPrefix(name, "snapshot-") ||
		strings.HasPrefix(name, "import-") ||
		strings.HasPrefix(name, "remote-backup-") ||
		strings.HasPrefix(name, "first-run-restore-")
}

func isLegacyDatabaseTemporaryFile(name string) bool {
	return (strings.HasPrefix(name, ".remote-backup-") && strings.HasSuffix(name, ".aipdb")) ||
		(strings.HasPrefix(name, ".first-run-restore-") && strings.HasSuffix(name, ".aipdb")) ||
		(strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".backup.aipdb"))
}
