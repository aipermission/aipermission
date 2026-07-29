package vaultsessions

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

var ErrUnauthorized = errors.New("Vault session authorization lease is missing, expired, or stale")

const MaxLeaseTTL = 12 * time.Hour

type Lease struct {
	WorkspaceID            string
	RuntimeInstanceID      string
	TokenID                int64
	RuntimeID              int64
	SessionID              int64
	SessionGeneration      int64
	ApprovalContextHash    string
	EnvironmentContentHash string
	ExpiresAt              time.Time
	Validate               func(context.Context) error
}

type Store struct {
	mu     sync.RWMutex
	leases map[leaseKey]Lease
	now    func() time.Time
}

type leaseKey struct {
	workspaceID            string
	runtimeInstanceID      string
	tokenID                int64
	runtimeID              int64
	sessionID              int64
	sessionGeneration      int64
	approvalContextHash    string
	environmentContentHash string
}

func NewStore() *Store {
	return &Store{leases: map[leaseKey]Lease{}, now: time.Now}
}

func (s *Store) Grant(lease Lease) error {
	if s == nil {
		return ErrUnauthorized
	}
	if lease.WorkspaceID == "" || lease.RuntimeInstanceID == "" || lease.TokenID < 1 ||
		lease.RuntimeID < 1 || lease.SessionID < 1 || lease.SessionGeneration < 1 ||
		lease.ApprovalContextHash == "" || lease.EnvironmentContentHash == "" {
		return ErrUnauthorized
	}
	now := s.now().UTC()
	maxExpiry := now.Add(MaxLeaseTTL)
	if lease.ExpiresAt.IsZero() || lease.ExpiresAt.After(maxExpiry) {
		lease.ExpiresAt = maxExpiry
	}
	if !lease.ExpiresAt.After(now) {
		return ErrUnauthorized
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeExpiredLocked(now)
	s.leases[keyFor(lease)] = lease
	return nil
}

func (s *Store) Authorize(ctx context.Context, principal executionprincipal.Principal, session console.SessionAuthorization, _ console.SessionOperation) error {
	if s == nil || !principal.IsMCPToken() || session.EnvironmentContentHash == "" || session.ApprovalContextHash == "" {
		return ErrUnauthorized
	}
	lease := Lease{
		WorkspaceID: principal.WorkspaceID, RuntimeInstanceID: principal.RuntimeInstanceID,
		TokenID: principal.TokenID, RuntimeID: session.Handle.RuntimeID,
		SessionID: session.Handle.ID, SessionGeneration: session.Handle.Generation,
		ApprovalContextHash:    session.ApprovalContextHash,
		EnvironmentContentHash: session.EnvironmentContentHash,
	}
	now := s.now().UTC()
	s.mu.Lock()
	s.removeExpiredLocked(now)
	item, ok := s.leases[keyFor(lease)]
	if !ok || !item.ExpiresAt.After(now) {
		s.mu.Unlock()
		return ErrUnauthorized
	}
	s.mu.Unlock()
	if item.Validate != nil {
		if err := item.Validate(ctx); err != nil {
			s.RevokeSession(session.Handle)
			return ErrUnauthorized
		}
	}
	s.mu.RLock()
	current, ok := s.leases[keyFor(lease)]
	s.mu.RUnlock()
	if !ok || current.ApprovalContextHash != item.ApprovalContextHash ||
		current.EnvironmentContentHash != item.EnvironmentContentHash ||
		!current.ExpiresAt.After(s.now().UTC()) {
		return ErrUnauthorized
	}
	return nil
}

func (s *Store) RevokeSession(handle console.SessionHandle) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.leases {
		if key.runtimeID == handle.RuntimeID && key.sessionID == handle.ID && key.sessionGeneration == handle.Generation {
			delete(s.leases, key)
		}
	}
}

func (s *Store) RevokeToken(tokenID int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.leases {
		if key.tokenID == tokenID {
			delete(s.leases, key)
		}
	}
}

func (s *Store) RevokeRuntime(runtimeID int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.leases {
		if key.runtimeID == runtimeID {
			delete(s.leases, key)
		}
	}
}

func (s *Store) Clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.leases)
}

func (s *Store) removeExpiredLocked(now time.Time) {
	for key, item := range s.leases {
		if !item.ExpiresAt.After(now) {
			delete(s.leases, key)
		}
	}
}

func keyFor(lease Lease) leaseKey {
	return leaseKey{
		workspaceID: lease.WorkspaceID, runtimeInstanceID: lease.RuntimeInstanceID,
		tokenID: lease.TokenID, runtimeID: lease.RuntimeID,
		sessionID: lease.SessionID, sessionGeneration: lease.SessionGeneration,
		approvalContextHash:    lease.ApprovalContextHash,
		environmentContentHash: lease.EnvironmentContentHash,
	}
}
