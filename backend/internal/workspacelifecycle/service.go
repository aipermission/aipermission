package workspacelifecycle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/db"
)

var (
	ErrLocked           = errors.New("database is locked")
	ErrNotInitialized   = errors.New("encrypted database is not initialized")
	ErrPasswordRequired = errors.New("database password is required")
	ErrPlaintext        = errors.New("plaintext SQLite databases are not supported")
	ErrInvalidScope     = errors.New("scope must be current or all")
	ErrCredential       = errors.New("database credential rejected")
	ErrNameRequired     = errors.New("database name is required")
	ErrNameConfirmation = errors.New("database name confirmation does not match")
	ErrRuntimeUnlocked  = errors.New("database is currently unlocked")
	ErrInvalidRequest   = errors.New("invalid workspace lifecycle request")
	ErrPasswordPolicy   = errors.New("database password policy rejected")
)

type Runtime interface {
	WorkspaceIdentity() Identity
	WorkspaceDatabase() *sql.DB
}

type Dependencies[T Runtime] struct {
	DataPath            string
	Registry            *Registry[T]
	Open                func(path, id, password string) (T, error)
	Close               func(T) error
	OnActivated         func(T)
	OnOpened            func(T)
	Validate            func(path, password string) error
	Move                func(currentPath, targetPath string) error
	Delete              func(path string) error
	ValidateNewPassword func(context.Context, *sql.DB, string, string) error
}

type Service[T Runtime] struct {
	mu                  sync.RWMutex
	dataPath            string
	registry            *Registry[T]
	open                func(path, id, password string) (T, error)
	close               func(T) error
	onActivated         func(T)
	onOpened            func(T)
	validate            func(path, password string) error
	move                func(currentPath, targetPath string) error
	delete              func(path string) error
	validateNewPassword func(context.Context, *sql.DB, string, string) error
}

type Status struct {
	State             string
	Identity          Identity
	DatabaseName      string
	DatabaseSizeBytes int64
	Databases         []databasecatalog.DatabaseInfo
}

type Transition[T Runtime] struct {
	Status   string
	State    string
	Identity Identity
	Runtime  T
	Opened   bool
}

type verifiedCredentialError struct{ err error }

func (e verifiedCredentialError) Error() string { return e.err.Error() }
func (e verifiedCredentialError) Unwrap() error { return e.err }

func CredentialWasVerified(err error) bool {
	var verified verifiedCredentialError
	return errors.As(err, &verified)
}

func afterCredential(err error) error {
	if err == nil {
		return nil
	}
	return verifiedCredentialError{err: err}
}

type classifiedError struct {
	kind error
	err  error
}

func (e classifiedError) Error() string { return e.err.Error() }
func (e classifiedError) Unwrap() error { return e.err }
func (e classifiedError) Is(target error) bool {
	return target == e.kind || errors.Is(e.err, target)
}

func classify(kind, err error) error { return classifiedError{kind: kind, err: err} }

func PasswordPolicyError(err error) error { return classify(ErrPasswordPolicy, err) }

func NewService[T Runtime](dependencies Dependencies[T]) (*Service[T], error) {
	if strings.TrimSpace(dependencies.DataPath) == "" || dependencies.Registry == nil ||
		dependencies.Open == nil || dependencies.Close == nil {
		return nil, fmt.Errorf("workspace lifecycle dependencies are incomplete")
	}
	validate := dependencies.Validate
	if validate == nil {
		validate = db.ValidateEncrypted
	}
	move := dependencies.Move
	if move == nil {
		move = databasecatalog.MoveDatabase
	}
	deleteDatabase := dependencies.Delete
	if deleteDatabase == nil {
		deleteDatabase = databasecatalog.DeleteDatabase
	}
	return &Service[T]{
		dataPath: dependencies.DataPath, registry: dependencies.Registry,
		open: dependencies.Open, close: dependencies.Close,
		onActivated: dependencies.OnActivated, onOpened: dependencies.OnOpened,
		validate: validate, move: move, delete: deleteDatabase,
		validateNewPassword: dependencies.ValidateNewPassword,
	}, nil
}

func (s *Service[T]) IsUnlocked() bool { return s.registry.IsUnlocked() }

func (s *Service[T]) Active() (T, bool) { return s.registry.Active() }

func (s *Service[T]) Selection() Identity { return s.registry.Selection() }

func (s *Service[T]) Snapshot() []T { return s.registry.Snapshot() }

func (s *Service[T]) Status() (Status, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statusLocked()
}

func (s *Service[T]) statusLocked() (Status, error) {
	selection := s.registry.Selection()
	databases, err := databasecatalog.ListDatabases(s.dataPath, selection.Path)
	if err != nil {
		return Status{}, fmt.Errorf("list encrypted databases: %w", err)
	}
	for index := range databases {
		if runtime, ok := s.registry.Lookup(databases[index].ID); ok && runtime.WorkspaceIdentity().Path == databases[index].Path {
			databases[index].Unlocked = true
		}
	}
	activeID := selection.ID
	activeName := databasecatalog.DefaultDatabaseName(s.dataPath)
	for _, item := range databases {
		if item.Path == selection.Path {
			activeID, activeName = item.ID, item.Name
			break
		}
		if item.ID == activeID {
			activeName = item.Name
		}
	}
	identity := selection
	identity.ID = activeID
	if _, ok := s.registry.Active(); ok {
		return Status{State: "unlocked", Identity: identity, DatabaseName: activeName,
			DatabaseSizeBytes: fileSize(selection.Path), Databases: databases}, nil
	}
	if len(databases) == 0 {
		return Status{State: "setup_required", Identity: identity, DatabaseName: activeName, Databases: databases}, nil
	}
	selected := databases[0]
	for _, item := range databases {
		if item.ID == activeID {
			selected = item
			break
		}
	}
	return Status{State: selected.State, Identity: Identity{ID: selected.ID, Path: selected.Path},
		DatabaseName: selected.Name, Databases: databases}, nil
}

func (s *Service[T]) Setup(databaseID, databaseName, password string) (Transition[T], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if runtime, ok := s.registry.Active(); ok {
		return s.transition("current", runtime, false), nil
	}
	identity, err := s.setupTarget(databaseID, databaseName)
	if err != nil {
		return Transition[T]{}, err
	}
	if db.Exists(identity.Path) {
		if db.LooksLikePlainSQLite(identity.Path) {
			return Transition[T]{}, ErrPlaintext
		}
		return Transition[T]{}, fmt.Errorf("encrypted database already exists; unlock it or create a new database")
	}
	s.registry.Select(identity)
	return s.openAndActivateLocked(identity, password, "unlocked")
}

func (s *Service[T]) Unlock(databaseID, password string) (Transition[T], error) {
	if password == "" {
		return Transition[T]{}, ErrPasswordRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, err := s.unlockTarget(databaseID)
	if err != nil {
		return Transition[T]{}, err
	}
	if runtime, ok := s.registry.Lookup(identity.ID); ok && runtime.WorkspaceIdentity().Path == identity.Path {
		if err := s.validate(identity.Path, password); err != nil {
			return Transition[T]{}, fmt.Errorf("%w: %v", ErrCredential, err)
		}
		s.activateLocked(runtime)
		return s.transition("unlocked", runtime, false), nil
	}
	if !db.Exists(identity.Path) {
		return Transition[T]{}, ErrNotInitialized
	}
	if db.LooksLikePlainSQLite(identity.Path) {
		return Transition[T]{}, ErrPlaintext
	}
	previous := s.registry.Selection()
	s.registry.Select(identity)
	transition, err := s.openAndActivateLocked(identity, password, "unlocked")
	if err != nil && s.registry.IsUnlocked() {
		s.registry.Select(previous)
	}
	return transition, err
}

func (s *Service[T]) Switch(databaseID, password string) (Transition[T], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.registry.IsUnlocked() {
		return Transition[T]{}, ErrLocked
	}
	identity, err := s.unlockTarget(databaseID)
	if err != nil {
		return Transition[T]{}, err
	}
	if active, ok := s.registry.Active(); ok {
		activeIdentity := active.WorkspaceIdentity()
		if identity.ID == activeIdentity.ID || identity.Path == activeIdentity.Path {
			return s.transition("current", active, false), nil
		}
	}
	if runtime, ok := s.registry.Lookup(identity.ID); ok && runtime.WorkspaceIdentity().Path == identity.Path {
		s.activateLocked(runtime)
		return s.transition("switched", runtime, false), nil
	}
	if password == "" {
		return Transition[T]{}, ErrPasswordRequired
	}
	if !db.Exists(identity.Path) {
		return Transition[T]{}, ErrNotInitialized
	}
	if db.LooksLikePlainSQLite(identity.Path) {
		return Transition[T]{}, ErrPlaintext
	}
	previous := s.registry.Selection()
	s.registry.Select(identity)
	transition, err := s.openAndActivateLocked(identity, password, "switched")
	if err != nil {
		s.registry.Select(previous)
	}
	return transition, err
}

func (s *Service[T]) Lock(scope string) (Status, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "current"
	}
	if scope != "current" && scope != "all" {
		return Status{}, ErrInvalidScope
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var closeErrors []error
	if scope == "all" {
		for _, runtime := range s.registry.Clear(databasecatalog.DefaultDatabaseID(s.dataPath)) {
			if err := s.close(runtime); err != nil {
				closeErrors = append(closeErrors, err)
			}
		}
	} else {
		selection := s.registry.Selection()
		if runtime, ok := s.registry.Lookup(selection.ID); ok {
			if err := s.close(runtime); err != nil {
				closeErrors = append(closeErrors, err)
			}
		}
		_, _, promoted, promotedOK := s.registry.Remove(selection.ID, true)
		if promotedOK {
			s.activateLocked(promoted)
		}
	}
	status, statusErr := s.statusLocked()
	return status, errors.Join(errors.Join(closeErrors...), statusErr)
}

func (s *Service[T]) WillLockAll(scope string) bool {
	return strings.TrimSpace(scope) == "all" || s.registry.Len() <= 1
}

func (s *Service[T]) CloseAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var closeErrors []error
	for _, runtime := range s.registry.Clear(databasecatalog.DefaultDatabaseID(s.dataPath)) {
		if err := s.close(runtime); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	return errors.Join(closeErrors...)
}

func (s *Service[T]) openAndActivateLocked(identity Identity, password, status string) (Transition[T], error) {
	runtime, err := s.open(identity.Path, identity.ID, password)
	if err != nil {
		return Transition[T]{}, err
	}
	s.activateLocked(runtime)
	if s.onOpened != nil {
		s.onOpened(runtime)
	}
	return s.transition(status, runtime, true), nil
}

func (s *Service[T]) activateLocked(runtime T) {
	s.registry.Activate(runtime)
	if s.onActivated != nil {
		s.onActivated(runtime)
	}
}

func (s *Service[T]) transition(status string, runtime T, opened bool) Transition[T] {
	return Transition[T]{Status: status, State: "unlocked", Identity: runtime.WorkspaceIdentity(), Runtime: runtime, Opened: opened}
}

func (s *Service[T]) Rename(ctx context.Context, databaseName, currentPassword string) (Transition[T], error) {
	databaseName = strings.TrimSpace(databaseName)
	if databaseName == "" {
		return Transition[T]{}, ErrNameRequired
	}
	if currentPassword == "" {
		return Transition[T]{}, ErrPasswordRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	runtime, ok := s.registry.Active()
	if !ok {
		return Transition[T]{}, ErrLocked
	}
	identity := runtime.WorkspaceIdentity()
	newID, newPath, err := databasecatalog.RenameDatabaseTarget(s.dataPath, identity.Path, databaseName)
	if err != nil {
		return Transition[T]{}, classify(ErrInvalidRequest, err)
	}
	if err := s.validate(identity.Path, currentPassword); err != nil {
		return Transition[T]{}, fmt.Errorf("%w: %v", ErrCredential, err)
	}
	if err := db.CheckpointForFilesystemMutation(ctx, runtime.WorkspaceDatabase()); err != nil {
		return Transition[T]{}, afterCredential(err)
	}
	closeErr := s.close(runtime)
	s.registry.Remove(identity.ID, false)
	if closeErr != nil {
		s.registry.Select(identity)
		s.reopenBestEffort(identity, currentPassword)
		return Transition[T]{}, afterCredential(closeErr)
	}
	if err := s.move(identity.Path, newPath); err != nil {
		s.registry.Select(identity)
		s.reopenBestEffort(identity, currentPassword)
		return Transition[T]{}, afterCredential(err)
	}
	identity = Identity{ID: newID, Path: newPath}
	s.registry.Select(identity)
	return Transition[T]{Status: "renamed", State: "locked", Identity: identity}, nil
}

func (s *Service[T]) DeleteCurrent(ctx context.Context, confirmName, currentPassword string) (Transition[T], error) {
	if currentPassword == "" {
		return Transition[T]{}, ErrPasswordRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	runtime, ok := s.registry.Active()
	if !ok {
		return Transition[T]{}, ErrLocked
	}
	identity := runtime.WorkspaceIdentity()
	if strings.TrimSpace(confirmName) != s.databaseNameLocked(identity) {
		return Transition[T]{}, ErrNameConfirmation
	}
	if err := s.validate(identity.Path, currentPassword); err != nil {
		return Transition[T]{}, fmt.Errorf("%w: %v", ErrCredential, err)
	}
	if err := db.CheckpointForFilesystemMutation(ctx, runtime.WorkspaceDatabase()); err != nil {
		return Transition[T]{}, afterCredential(err)
	}
	closeErr := s.close(runtime)
	_, _, promoted, promotedOK := s.registry.Remove(identity.ID, true)
	if promotedOK {
		s.activateLocked(promoted)
	}
	if closeErr != nil {
		return Transition[T]{}, afterCredential(closeErr)
	}
	if err := s.delete(identity.Path); err != nil {
		return Transition[T]{}, afterCredential(err)
	}
	if promotedOK {
		return s.transition("deleted", promoted, false), nil
	}
	s.registry.ResetSelection(databasecatalog.DefaultDatabaseID(s.dataPath))
	return Transition[T]{Status: "deleted", State: "locked", Identity: s.registry.Selection()}, nil
}

func (s *Service[T]) DeleteLocked(databaseID, password string) (Transition[T], error) {
	if strings.TrimSpace(databaseID) == "" {
		return Transition[T]{}, fmt.Errorf("database id is required")
	}
	if password == "" {
		return Transition[T]{}, ErrPasswordRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, err := s.unlockTarget(databaseID)
	if err != nil {
		return Transition[T]{}, classify(ErrInvalidRequest, err)
	}
	if _, ok := s.registry.Lookup(identity.ID); ok {
		return Transition[T]{}, ErrRuntimeUnlocked
	}
	if !db.Exists(identity.Path) {
		return Transition[T]{}, ErrNotInitialized
	}
	if db.LooksLikePlainSQLite(identity.Path) {
		return Transition[T]{}, classify(ErrPlaintext, fmt.Errorf("plaintext SQLite databases are not supported; remove this file manually"))
	}
	if err := s.validate(identity.Path, password); err != nil {
		return Transition[T]{}, fmt.Errorf("%w: %v", ErrCredential, err)
	}
	if err := s.delete(identity.Path); err != nil {
		return Transition[T]{}, afterCredential(err)
	}
	if s.registry.Selection().ID == identity.ID {
		s.registry.ResetSelection(databasecatalog.DefaultDatabaseID(s.dataPath))
	}
	return Transition[T]{Status: "deleted", State: "locked", Identity: identity}, nil
}

func (s *Service[T]) ChangePassword(ctx context.Context, currentPassword, newPassword string) error {
	if currentPassword == "" || newPassword == "" {
		return ErrPasswordRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	runtime, ok := s.registry.Active()
	if !ok || runtime.WorkspaceDatabase() == nil {
		return ErrLocked
	}
	identity := runtime.WorkspaceIdentity()
	if s.validateNewPassword != nil {
		if err := s.validateNewPassword(ctx, runtime.WorkspaceDatabase(), s.databaseNameLocked(identity), newPassword); err != nil {
			return err
		}
	}
	if err := s.validate(identity.Path, currentPassword); err != nil {
		return fmt.Errorf("%w: %v", ErrCredential, err)
	}
	_ = db.CheckpointFull(ctx, runtime.WorkspaceDatabase())
	if err := db.Rekey(runtime.WorkspaceDatabase(), newPassword); err != nil {
		return afterCredential(err)
	}
	_ = db.CheckpointFull(ctx, runtime.WorkspaceDatabase())
	if err := s.validate(identity.Path, newPassword); err != nil {
		return afterCredential(fmt.Errorf("database password changed but verification reopen failed: %w", err))
	}
	return nil
}

func (s *Service[T]) databaseNameLocked(identity Identity) string {
	items, err := databasecatalog.ListDatabases(s.dataPath, identity.Path)
	if err != nil {
		return identity.ID
	}
	for _, item := range items {
		if item.Path == identity.Path || item.ID == identity.ID {
			return item.Name
		}
	}
	return identity.ID
}

func (s *Service[T]) reopenBestEffort(identity Identity, password string) {
	runtime, err := s.open(identity.Path, identity.ID, password)
	if err != nil {
		return
	}
	s.activateLocked(runtime)
	if s.onOpened != nil {
		s.onOpened(runtime)
	}
}

func (s *Service[T]) setupTarget(databaseID, databaseName string) (Identity, error) {
	databaseID, databaseName = strings.TrimSpace(databaseID), strings.TrimSpace(databaseName)
	if databaseName != "" {
		id, path, err := databasecatalog.NewDatabasePath(s.dataPath, databaseName)
		return Identity{ID: id, Path: path}, err
	}
	path, err := databasecatalog.DatabasePath(s.dataPath, databaseID)
	if databaseID == "" {
		databaseID = databasecatalog.DefaultDatabaseID(s.dataPath)
	}
	return Identity{ID: databaseID, Path: path}, err
}

func (s *Service[T]) unlockTarget(databaseID string) (Identity, error) {
	databaseID = strings.TrimSpace(databaseID)
	if databaseID == "" {
		databaseID = s.registry.Selection().ID
	}
	path, err := databasecatalog.DatabasePath(s.dataPath, databaseID)
	if databaseID == "" {
		databaseID = databasecatalog.DefaultDatabaseID(s.dataPath)
	}
	return Identity{ID: databaseID, Path: path}, err
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
