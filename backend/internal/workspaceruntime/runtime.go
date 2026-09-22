package workspaceruntime

import (
	"context"
	"errors"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/foundation"
	connectorstate "github.com/aipermission/aipermission/backend/internal/workspaceruntime/state/connectors"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/state/observation"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/state/security"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/state/storage"
)

func TagActionIdentity(runtime *Runtime, canonical []byte) (string, error) {
	if runtime == nil {
		return "", errors.New("workspace action identity is unavailable")
	}
	runtime.actionIdentityMu.RLock()
	defer runtime.actionIdentityMu.RUnlock()
	return actions.IdentityTag(runtime.ActionIdentityKey, canonical)
}

func (runtime *Runtime) ClearActionIdentity() {
	if runtime == nil {
		return
	}
	runtime.actionIdentityMu.Lock()
	actions.ClearIdentityKey(runtime.ActionIdentityKey)
	runtime.ActionIdentityKey = nil
	runtime.actionIdentityMu.Unlock()
}

func (runtime *Runtime) HasActionIdentity() bool {
	if runtime == nil {
		return false
	}
	runtime.actionIdentityMu.RLock()
	defer runtime.actionIdentityMu.RUnlock()
	return runtime.ActionIdentityKey != nil
}

type Runtime struct {
	ID                string
	Path              string
	GatewaySecret     string
	WorkspaceUUID     string
	UIRetryIdentity   string
	RuntimeInstanceID string
	ActionIdentityKey []byte
	Storage           storage.State
	Connectors        connectorstate.State
	Security          *security.State
	Observation       observation.State
	actionIdentityMu  sync.RWMutex
	teardownMu        sync.Mutex
	teardownOnce      sync.Once
	teardownDone      chan struct{}
}

// StartTeardown admits exactly one persistent teardown coordinator for this
// runtime. The coordinator owns the runtime until encrypted storage closes.
func (runtime *Runtime) StartTeardown(run func()) <-chan struct{} {
	if runtime == nil || run == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	runtime.teardownMu.Lock()
	if runtime.teardownDone == nil {
		runtime.teardownDone = make(chan struct{})
	}
	done := runtime.teardownDone
	runtime.teardownMu.Unlock()
	runtime.teardownOnce.Do(func() {
		go func() {
			defer close(done)
			run()
		}()
	})
	return done
}

func (runtime *Runtime) WaitTeardown(ctx context.Context) error {
	if runtime == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runtime.teardownMu.Lock()
	done := runtime.teardownDone
	runtime.teardownMu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func New(state foundation.State) *Runtime {
	return &Runtime{
		ID: state.ID, Path: state.Path,
		GatewaySecret:     state.Identity.GatewaySecret,
		WorkspaceUUID:     state.Identity.WorkspaceUUID,
		UIRetryIdentity:   state.Identity.UIRetryIdentity,
		RuntimeInstanceID: state.Identity.RuntimeInstanceID,
		ActionIdentityKey: state.Identity.ActionIdentityKey,
		Storage: storage.New(
			state.Database, state.Identity.Vault, state.TokenStore, state.Identity.WorkspaceUUID, state.Ownership,
		),
		Connectors: connectorstate.New(
			state.Registry, state.AdapterRegistry, state.Database, state.Identity.Vault, state.Identity.WorkspaceUUID,
		),
		Security: security.New(state.Database),
	}
}
