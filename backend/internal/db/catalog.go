package db

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var ErrDatabaseExists = errors.New("database name already exists")

type DatabaseInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	State    string `json:"state"`
	Current  bool   `json:"current"`
	Unlocked bool   `json:"unlocked"`
}

func ListDatabases(defaultPath string, currentPath string) ([]DatabaseInfo, error) {
	items := []DatabaseInfo{}
	if Exists(defaultPath) {
		items = append(items, databaseInfo(DefaultDatabaseID(defaultPath), DefaultDatabaseName(defaultPath), defaultPath, currentPath))
	}

	dir := DatabasesDir(defaultPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return items, nil
		}
		return nil, fmt.Errorf("list databases: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".db" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".db")
		path := filepath.Join(dir, entry.Name())
		items = append(items, databaseInfo(id, displayDatabaseName(id), path, currentPath))
	}
	return items, nil
}

func DatabasePath(defaultPath string, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || id == "local-default" {
		return defaultPath, nil
	}
	if !validDatabaseID(id) {
		return "", fmt.Errorf("invalid database id")
	}
	namedPath := filepath.Join(DatabasesDir(defaultPath), id+".db")
	if id == "default" && !Exists(namedPath) {
		return defaultPath, nil
	}
	return namedPath, nil
}

func DefaultDatabaseID(defaultPath string) string {
	if Exists(filepath.Join(DatabasesDir(defaultPath), "default.db")) {
		return "local-default"
	}
	return "default"
}

func DefaultDatabaseName(defaultPath string) string {
	if DefaultDatabaseID(defaultPath) == "local-default" {
		return "Local Default"
	}
	return "Default"
}

func NewDatabasePath(defaultPath string, name string) (string, string, error) {
	id := slugifyDatabaseName(name)
	if id == "" {
		return "", "", fmt.Errorf("database name is required")
	}
	dir := DatabasesDir(defaultPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("create databases directory: %w", err)
	}
	path := filepath.Join(dir, id+".db")
	for i := 2; Exists(path); i++ {
		nextID := fmt.Sprintf("%s-%d", id, i)
		path = filepath.Join(dir, nextID+".db")
		if !Exists(path) {
			id = nextID
			break
		}
	}
	return id, path, nil
}

func NewDatabasePathExact(defaultPath string, name string) (string, string, error) {
	id := slugifyDatabaseName(name)
	if id == "" {
		return "", "", fmt.Errorf("database name is required")
	}
	items, err := ListDatabases(defaultPath, "")
	if err != nil {
		return "", "", err
	}
	for _, item := range items {
		if slugifyDatabaseName(item.Name) == id {
			return "", "", ErrDatabaseExists
		}
	}
	dir := DatabasesDir(defaultPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("create databases directory: %w", err)
	}
	path := filepath.Join(dir, id+".db")
	if Exists(path) {
		return "", "", ErrDatabaseExists
	}
	return id, path, nil
}

func RenameDatabase(defaultPath string, currentPath string, name string) (string, string, error) {
	id, targetPath, err := RenameDatabaseTarget(defaultPath, currentPath, name)
	if err != nil {
		return "", "", err
	}
	if err := MoveDatabase(currentPath, targetPath); err != nil {
		return "", "", err
	}
	return id, targetPath, nil
}

func RenameDatabaseTarget(defaultPath string, currentPath string, name string) (string, string, error) {
	id := slugifyDatabaseName(name)
	if id == "" {
		return "", "", fmt.Errorf("database name is required")
	}
	targetPath := filepath.Join(DatabasesDir(defaultPath), id+".db")
	if currentPath == targetPath {
		return "", "", fmt.Errorf("database already has this name")
	}
	if Exists(targetPath) {
		return "", "", fmt.Errorf("database name already exists")
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return "", "", fmt.Errorf("create databases directory: %w", err)
	}
	return id, targetPath, nil
}

func MoveDatabase(currentPath string, targetPath string) error {
	if err := os.Rename(currentPath, targetPath); err != nil {
		return fmt.Errorf("rename database: %w", err)
	}
	_ = os.Remove(currentPath + "-wal")
	_ = os.Remove(currentPath + "-shm")
	return nil
}

func DeleteDatabase(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete database: %w", err)
	}
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
	return nil
}

func DatabasesDir(defaultPath string) string {
	return filepath.Join(filepath.Dir(defaultPath), "databases")
}

func databaseInfo(id string, name string, path string, currentPath string) DatabaseInfo {
	state := "locked"
	if LooksLikePlainSQLite(path) {
		state = "unsupported_plaintext"
	}
	return DatabaseInfo{
		ID:      id,
		Name:    name,
		Path:    path,
		State:   state,
		Current: path == currentPath,
	}
}

func displayDatabaseName(id string) string {
	if id == "default" {
		return "Default"
	}
	return strings.ReplaceAll(id, "-", " ")
}

var databaseIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func validDatabaseID(id string) bool {
	return databaseIDPattern.MatchString(id)
}

func slugifyDatabaseName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	builder := strings.Builder{}
	lastDash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && builder.Len() > 0 {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
