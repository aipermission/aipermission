package workspacelifecycle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

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
	ErrAuthentication   = errors.New("database authentication failed")
	ErrInitialization   = errors.New("database initialization failed")
)

type Runtime interface {
	WorkspaceIdentity() Identity
	WorkspaceDatabase() *sql.DB
}

type Dependencies[T Runtime] struct {
	DataPath            string
	Registry            *Registry[T]
	Open                func(context.Context, string, string, string) (T, error)
	Close               func(T) error
	WaitClosed          func(context.Context, T) error
	IsOwned             func(Identity) bool
	OnActivated         func(T)
	OnOpened            func(T)
	Validate            func(path, password string) error
	Move                func(currentPath, targetPath string) error
	Delete              func(path string) error
	ValidateNewPassword func(context.Context, T, string, string) error
	Publish             func(sourcePath, targetPath string) error
	GatewaySecret       func() string
}

type Service[T Runtime] struct {
	gate                *requestGate
	mu                  sync.RWMutex
	dataPath            string
	registry            *Registry[T]
	open                func(context.Context, string, string, string) (T, error)
	close               func(T) error
	waitClosed          func(context.Context, T) error
	isOwned             func(Identity) bool
	onActivated         func(T)
	onOpened            func(T)
	validate            func(path, password string) error
	move                func(currentPath, targetPath string) error
	delete              func(path string) error
	validateNewPassword func(context.Context, T, string, string) error
	publish             func(sourcePath, targetPath string) error
	gatewaySecret       func() string
}

func (s *Service[T]) AcquireReadContext(ctx context.Context) (func(), error) {
	return s.gate.acquireRead(ctx)
}

func (s *Service[T]) AcquireMutationContext(ctx context.Context) (func(), error) {
	return s.gate.acquireMutation(ctx)
}

type Status struct {
	State             string
	Identity          Identity
	DatabaseName      string
	DatabaseSizeBytes int64
	Databases         []databasecatalog.DatabaseInfo
}

type Transition struct {
	Status   string
	State    string
	Identity Identity
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

type deferredWorkspaceClose interface{ WorkspaceCloseDeferred() }

func closeWasDeferred(err error) bool {
	var deferred deferredWorkspaceClose
	return errors.As(err, &deferred)
}

func (e classifiedError) Error() string { return e.err.Error() }
func (e classifiedError) Unwrap() error { return e.err }
func (e classifiedError) Is(target error) bool {
	return target == e.kind || errors.Is(e.err, target)
}

func classify(kind, err error) error { return classifiedError{kind: kind, err: err} }

func PasswordPolicyError(err error) error { return classify(ErrPasswordPolicy, err) }

func ValidatePassword(password, confirmation string) error {
	if len(password) < 14 {
		return fmt.Errorf("password must be at least 14 characters")
	}
	if password != confirmation {
		return fmt.Errorf("password confirmation does not match")
	}
	var hasUpper, hasLower, hasDigit bool
	for _, char := range password {
		switch {
		case char >= 'A' && char <= 'Z':
			hasUpper = true
		case char >= 'a' && char <= 'z':
			hasLower = true
		case char >= '0' && char <= '9':
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		return fmt.Errorf("password must include uppercase letters, lowercase letters, and numbers")
	}
	return nil
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
	move := dependencies.Move
	if move == nil {
		move = databasecatalog.MoveDatabase
	}
	deleteDatabase := dependencies.Delete
	if deleteDatabase == nil {
		deleteDatabase = databasecatalog.DeleteDatabase
	}
	publish := dependencies.Publish
	if publish == nil {
		publish = db.PublishFileNoReplace
	}
	return &Service[T]{
		gate:     newRequestGate(),
		dataPath: dependencies.DataPath, registry: dependencies.Registry,
		open: dependencies.Open, close: dependencies.Close,
		waitClosed: dependencies.WaitClosed, isOwned: dependencies.IsOwned,
		onActivated: dependencies.OnActivated, onOpened: dependencies.OnOpened,
		validate: validate, move: move, delete: deleteDatabase,
		validateNewPassword: dependencies.ValidateNewPassword,
		publish:             publish, gatewaySecret: dependencies.GatewaySecret,
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

func (s *Service[T]) Setup(ctx context.Context, databaseID, databaseName, password string) (Transition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Transition{}, err
	}
	if runtime, ok := s.registry.Active(); ok {
		return s.transition("current", runtime, false), nil
	}
	identity, err := s.setupTarget(databaseID, databaseName)
	if err != nil {
		return Transition{}, err
	}
	if db.Exists(identity.Path) {
		if db.LooksLikePlainSQLite(identity.Path) {
			return Transition{}, ErrPlaintext
		}
		return Transition{}, fmt.Errorf("encrypted database already exists; unlock it or create a new database")
	}
	s.registry.Select(identity)
	return s.openAndActivateLocked(ctx, identity, password, "unlocked")
}

func (s *Service[T]) Unlock(ctx context.Context, databaseID, password string) (Transition, error) {
	if password == "" {
		return Transition{}, ErrPasswordRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Transition{}, err
	}
	identity, err := s.unlockTarget(databaseID)
	if err != nil {
		return Transition{}, err
	}
	if runtime, ok := s.registry.Lookup(identity.ID); ok && runtime.WorkspaceIdentity().Path == identity.Path {
		if err := s.validate(identity.Path, password); err != nil {
			return Transition{}, fmt.Errorf("%w: %v", ErrCredential, err)
		}
		s.activateLocked(runtime)
		return s.transition("unlocked", runtime, false), nil
	}
	if !db.Exists(identity.Path) {
		return Transition{}, ErrNotInitialized
	}
	if db.LooksLikePlainSQLite(identity.Path) {
		return Transition{}, ErrPlaintext
	}
	previous := s.registry.Selection()
	s.registry.Select(identity)
	transition, err := s.openAndActivateLocked(ctx, identity, password, "unlocked")
	if err != nil && s.registry.IsUnlocked() {
		s.registry.Select(previous)
	}
	return transition, err
}

func (s *Service[T]) Switch(ctx context.Context, databaseID, password string) (Transition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Transition{}, err
	}
	if !s.registry.IsUnlocked() {
		return Transition{}, ErrLocked
	}
	identity, err := s.unlockTarget(databaseID)
	if err != nil {
		return Transition{}, err
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
		return Transition{}, ErrPasswordRequired
	}
	if !db.Exists(identity.Path) {
		return Transition{}, ErrNotInitialized
	}
	if db.LooksLikePlainSQLite(identity.Path) {
		return Transition{}, ErrPlaintext
	}
	previous := s.registry.Selection()
	s.registry.Select(identity)
	transition, err := s.openAndActivateLocked(ctx, identity, password, "switched")
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
			if err := s.close(runtime); err != nil && !closeWasDeferred(err) {
				closeErrors = append(closeErrors, err)
			}
		}
	} else {
		selection := s.registry.Selection()
		if runtime, ok := s.registry.Lookup(selection.ID); ok {
			if err := s.close(runtime); err != nil && !closeWasDeferred(err) {
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

func (s *Service[T]) CloseAll(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	runtimes := s.registry.Clear(databasecatalog.DefaultDatabaseID(s.dataPath))
	s.mu.Unlock()

	results := make(chan error, len(runtimes))
	for _, runtime := range runtimes {
		go func(runtime T) {
			err := s.close(runtime)
			if closeWasDeferred(err) {
				err = nil
			}
			results <- err
		}(runtime)
	}
	closeErrors := make([]error, 0, len(runtimes)+1)
	for range runtimes {
		select {
		case err := <-results:
			if err != nil {
				closeErrors = append(closeErrors, err)
			}
		case <-ctx.Done():
			closeErrors = append(closeErrors, ctx.Err())
			return errors.Join(closeErrors...)
		}
	}
	return errors.Join(closeErrors...)
}

func (s *Service[T]) openAndActivateLocked(ctx context.Context, identity Identity, password, status string) (Transition, error) {
	runtime, err := s.open(ctx, identity.Path, identity.ID, password)
	if err != nil {
		return Transition{}, err
	}
	if err := ctx.Err(); err != nil {
		return Transition{}, errors.Join(err, s.close(runtime))
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

func (s *Service[T]) transition(status string, runtime T, opened bool) Transition {
	return Transition{Status: status, State: "unlocked", Identity: runtime.WorkspaceIdentity(), Opened: opened}
}

func (s *Service[T]) waitForDeferredClose(ctx context.Context, runtime T, closeErr error) error {
	if !closeWasDeferred(closeErr) {
		return closeErr
	}
	if s.waitClosed == nil {
		return errors.Join(closeErr, fmt.Errorf("deferred workspace close cannot be observed"))
	}
	if err := s.waitClosed(ctx, runtime); err != nil {
		return errors.Join(closeErr, fmt.Errorf("wait for deferred workspace close: %w", err))
	}
	return nil
}

func (s *Service[T]) Rename(ctx context.Context, databaseName, currentPassword string) (Transition, error) {
	databaseName = strings.TrimSpace(databaseName)
	if databaseName == "" {
		return Transition{}, ErrNameRequired
	}
	if currentPassword == "" {
		return Transition{}, ErrPasswordRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	runtime, ok := s.registry.Active()
	if !ok {
		return Transition{}, ErrLocked
	}
	identity := runtime.WorkspaceIdentity()
	newID, newPath, err := databasecatalog.RenameDatabaseTarget(s.dataPath, identity.Path, databaseName)
	if err != nil {
		return Transition{}, classify(ErrInvalidRequest, err)
	}
	if err := s.validate(identity.Path, currentPassword); err != nil {
		return Transition{}, fmt.Errorf("%w: %v", ErrCredential, err)
	}
	if err := db.CheckpointForFilesystemMutation(ctx, runtime.WorkspaceDatabase()); err != nil {
		return Transition{}, afterCredential(err)
	}
	closeErr := s.close(runtime)
	s.registry.Remove(identity.ID, false)
	if err := s.waitForDeferredClose(ctx, runtime, closeErr); err != nil {
		s.registry.Select(identity)
		if !closeWasDeferred(closeErr) {
			s.reopenBestEffort(identity, currentPassword)
		}
		return Transition{}, afterCredential(err)
	}
	if err := s.move(identity.Path, newPath); err != nil {
		s.registry.Select(identity)
		s.reopenBestEffort(identity, currentPassword)
		return Transition{}, afterCredential(err)
	}
	identity = Identity{ID: newID, Path: newPath}
	s.registry.Select(identity)
	return Transition{Status: "renamed", State: "locked", Identity: identity}, nil
}

func (s *Service[T]) DeleteCurrent(ctx context.Context, confirmName, currentPassword string) (Transition, error) {
	if currentPassword == "" {
		return Transition{}, ErrPasswordRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	runtime, ok := s.registry.Active()
	if !ok {
		return Transition{}, ErrLocked
	}
	identity := runtime.WorkspaceIdentity()
	if strings.TrimSpace(confirmName) != s.databaseNameLocked(identity) {
		return Transition{}, ErrNameConfirmation
	}
	if err := s.validate(identity.Path, currentPassword); err != nil {
		return Transition{}, fmt.Errorf("%w: %v", ErrCredential, err)
	}
	if err := db.CheckpointForFilesystemMutation(ctx, runtime.WorkspaceDatabase()); err != nil {
		return Transition{}, afterCredential(err)
	}
	closeErr := s.close(runtime)
	_, _, promoted, promotedOK := s.registry.Remove(identity.ID, true)
	if promotedOK {
		s.activateLocked(promoted)
	}
	if err := s.waitForDeferredClose(ctx, runtime, closeErr); err != nil {
		return Transition{}, afterCredential(err)
	}
	if err := s.delete(identity.Path); err != nil {
		return Transition{}, afterCredential(err)
	}
	if promotedOK {
		return s.transition("deleted", promoted, false), nil
	}
	s.registry.ResetSelection(databasecatalog.DefaultDatabaseID(s.dataPath))
	return Transition{Status: "deleted", State: "locked", Identity: s.registry.Selection()}, nil
}

func (s *Service[T]) DeleteLocked(databaseID, password string) (Transition, error) {
	if strings.TrimSpace(databaseID) == "" {
		return Transition{}, fmt.Errorf("database id is required")
	}
	if password == "" {
		return Transition{}, ErrPasswordRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, err := s.unlockTarget(databaseID)
	if err != nil {
		return Transition{}, classify(ErrInvalidRequest, err)
	}
	if _, ok := s.registry.Lookup(identity.ID); ok {
		return Transition{}, ErrRuntimeUnlocked
	}
	if s.isOwned != nil && s.isOwned(identity) {
		return Transition{}, ErrRuntimeUnlocked
	}
	if !db.Exists(identity.Path) {
		return Transition{}, ErrNotInitialized
	}
	if db.LooksLikePlainSQLite(identity.Path) {
		return Transition{}, classify(ErrPlaintext, fmt.Errorf("plaintext SQLite databases are not supported; remove this file manually"))
	}
	if err := s.validate(identity.Path, password); err != nil {
		return Transition{}, fmt.Errorf("%w: %v", ErrCredential, err)
	}
	if err := s.delete(identity.Path); err != nil {
		return Transition{}, afterCredential(err)
	}
	if s.registry.Selection().ID == identity.ID {
		s.registry.ResetSelection(databasecatalog.DefaultDatabaseID(s.dataPath))
	}
	return Transition{Status: "deleted", State: "locked", Identity: identity}, nil
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
		if err := s.validateNewPassword(ctx, runtime, s.databaseNameLocked(identity), newPassword); err != nil {
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runtime, err := s.open(ctx, identity.Path, identity.ID, password)
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
