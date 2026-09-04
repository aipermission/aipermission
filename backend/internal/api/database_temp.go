package api

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const databaseTempDirectoryName = ".aipermission-temp"

func reserveDatabaseTempPath(databasePath, pattern string) (string, error) {
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

func scavengeDatabaseTempPaths(defaultPath string, now time.Time) {
	directories := []string{
		filepath.Dir(defaultPath),
		filepath.Join(filepath.Dir(defaultPath), "databases"),
		filepath.Join(filepath.Dir(defaultPath), databaseTempDirectoryName),
		filepath.Join(filepath.Dir(defaultPath), "databases", databaseTempDirectoryName),
	}
	for _, directory := range directories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("inspect stale database temporary files path=%q error=%v", directory, err)
			}
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !isDatabaseTemporaryFile(entry.Name()) {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || now.Sub(info.ModTime()) < 24*time.Hour {
				continue
			}
			if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil && !os.IsNotExist(err) {
				log.Printf("remove stale database temporary file path=%q error=%v", filepath.Join(directory, entry.Name()), err)
			}
		}
	}
}

func isDatabaseTemporaryFile(name string) bool {
	return strings.HasPrefix(name, "snapshot-") ||
		strings.HasPrefix(name, "import-") ||
		strings.HasPrefix(name, "remote-backup-") ||
		strings.HasPrefix(name, "first-run-restore-") ||
		strings.HasPrefix(name, ".remote-backup-") ||
		strings.HasPrefix(name, ".first-run-restore-") ||
		(strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".backup.aipdb"))
}
