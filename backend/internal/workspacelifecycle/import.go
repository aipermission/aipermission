package workspacelifecycle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

var (
	ErrDatabaseExists    = errors.New("database name already exists")
	ErrUnsupportedSchema = errors.New("database schema is unsupported")
)

type ImportInput struct {
	DatabaseName  string
	Password      string
	Write         func(string) error
	Mutate        func(*sql.DB) error
	BeforePublish func() error
}

func (s *Service[T]) Import(ctx context.Context, input ImportInput) (Transition[T], error) {
	input.DatabaseName = strings.TrimSpace(input.DatabaseName)
	if input.DatabaseName == "" {
		return Transition[T]{}, ErrNameRequired
	}
	if input.Password == "" {
		return Transition[T]{}, ErrPasswordRequired
	}
	if input.Write == nil {
		return Transition[T]{}, fmt.Errorf("database import writer is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	targetID, targetPath, err := databasecatalog.NewDatabasePathExact(s.dataPath, input.DatabaseName)
	if err != nil {
		if errors.Is(err, databasecatalog.ErrDatabaseExists) {
			return Transition[T]{}, ErrDatabaseExists
		}
		return Transition[T]{}, classify(ErrInvalidRequest, err)
	}
	if err := databasecatalog.DeleteDatabase(targetPath + ".import"); err != nil {
		return Transition[T]{}, err
	}
	tmpPath, err := databasecatalog.ReserveTempPath(targetPath, "import-*.aipdb")
	if err != nil {
		return Transition[T]{}, err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return Transition[T]{}, err
	}
	if err := databasecatalog.DeleteDatabase(tmpPath); err != nil {
		return Transition[T]{}, err
	}
	defer cleanupImportCandidate(tmpPath)
	if err := input.Write(tmpPath); err != nil {
		return Transition[T]{}, err
	}
	if db.LooksLikePlainSQLite(tmpPath) {
		return Transition[T]{}, classify(ErrPlaintext, fmt.Errorf("plaintext SQLite imports are not supported; import an encrypted .aipdb database"))
	}
	candidate, err := db.OpenEncryptedImportCandidate(tmpPath, input.Password)
	if err != nil {
		if message := db.UnsupportedSchemaMessage(err); message != "" {
			return Transition[T]{}, afterCredential(classify(ErrUnsupportedSchema, errors.New(message)))
		}
		return Transition[T]{}, fmt.Errorf("%w: invalid database password or database file", ErrCredential)
	}
	if err := s.prepareImportCandidate(ctx, candidate, input.Mutate); err != nil {
		if closeErr := closeImportCandidate(candidate); closeErr != nil {
			log.Printf("failed closing rejected import candidate path=%q error=%v", tmpPath, closeErr)
		}
		return Transition[T]{}, afterCredential(err)
	}
	if err := closeImportCandidate(candidate); err != nil {
		return Transition[T]{}, afterCredential(err)
	}
	if db.Exists(targetPath) {
		return Transition[T]{}, afterCredential(ErrDatabaseExists)
	}
	if input.BeforePublish != nil {
		if err := input.BeforePublish(); err != nil {
			return Transition[T]{}, afterCredential(err)
		}
	}
	if err := s.publish(tmpPath, targetPath); err != nil {
		if errors.Is(err, db.ErrPublishTargetExists) {
			return Transition[T]{}, afterCredential(ErrDatabaseExists)
		}
		return Transition[T]{}, afterCredential(err)
	}
	previous := s.registry.Selection()
	identity := Identity{ID: targetID, Path: targetPath}
	s.registry.Select(identity)
	transition, err := s.openAndActivateLocked(identity, input.Password, "imported")
	if err == nil {
		return transition, nil
	}
	s.registry.Select(previous)
	if runtime, ok := s.registry.Lookup(previous.ID); ok {
		s.activateLocked(runtime)
	}
	if cleanupErr := databasecatalog.DeleteDatabase(targetPath); cleanupErr != nil {
		log.Printf("failed imported database cleanup path=%q error=%v", targetPath, cleanupErr)
	}
	return Transition[T]{}, afterCredential(err)
}

func (s *Service[T]) prepareImportCandidate(ctx context.Context, candidate *sql.DB, mutate func(*sql.DB) error) error {
	gatewaySecret := ""
	if s.gatewaySecret != nil {
		gatewaySecret = s.gatewaySecret()
	}
	if _, err := projectvault.ResolveGatewaySecret(ctx, candidate, gatewaySecret); err != nil {
		return classify(ErrInvalidRequest, err)
	}
	if _, err := projectvault.RotateUIRetryIdentity(ctx, candidate); err != nil {
		return err
	}
	if mutate != nil {
		return mutate(candidate)
	}
	return nil
}

func closeImportCandidate(database *sql.DB) error {
	if err := db.CheckpointForFilesystemMutation(context.Background(), database); err != nil {
		_ = database.Close()
		return fmt.Errorf("checkpoint import candidate: %w", err)
	}
	if err := database.Close(); err != nil {
		return fmt.Errorf("close import candidate: %w", err)
	}
	return nil
}

func cleanupImportCandidate(path string) {
	for _, candidate := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
		if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
			log.Printf("failed import candidate cleanup path=%q error=%v", candidate, err)
		}
	}
}
