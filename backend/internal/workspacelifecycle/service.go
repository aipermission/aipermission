package workspacelifecycle

import (
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
)

type Runtime interface {
	WorkspaceIdentity() Identity
	WorkspaceDatabase() *sql.DB
	WorkspaceGatewaySecret() string
}

type Dependencies[T Runtime] struct {
	DataPath    string
	Registry    *Registry[T]
	Open        func(path, id, password string) (T, error)
	Close       func(T) error
	OnActivated func(T)
	OnOpened    func(T)
	Validate    func(path, password string) error
}

type Service[T Runtime] struct {
	mu          sync.RWMutex
	dataPath    string
	registry    *Registry[T]
	open        func(path, id, password string) (T, error)
	close       func(T) error
	onActivated func(T)
	onOpened    func(T)
	validate    func(path, password string) error
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

func NewService[T Runtime](dependencies Dependencies[T]) (*Service[T], error) {
	if strings.TrimSpace(dependencies.DataPath) == "" || dependencies.Registry == nil ||
		dependencies.Open == nil || dependencies.Close == nil {
		return nil, fmt.Errorf("workspace lifecycle dependencies are incomplete")
	}
	validate := dependencies.Validate
	if validate == nil {
		validate = db.ValidateEncrypted
	}
	return &Service[T]{
		dataPath: dependencies.DataPath, registry: dependencies.Registry,
		open: dependencies.Open, close: dependencies.Close,
		onActivated: dependencies.OnActivated, onOpened: dependencies.OnOpened,
		validate: validate,
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
