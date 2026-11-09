package console

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/console/capacity"
	"github.com/aipermission/aipermission/backend/internal/console/idlecleanup"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

func (m *Manager) ConfigureIdleCleanup(workspaceID, runtimeID string, observe func(context.Context, string, any) error) {
	principal, err := executionprincipal.LocalOperator(workspaceID, runtimeID)
	if err != nil {
		return
	}
	if !principal.IsLocalOperator() || observe == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.idleCleanup != nil {
		return
	}
	m.idleCleanup = idlecleanup.New(m.db, idlecleanup.Ports{
		Snapshot: m.Capacity,
		Close: func(ctx context.Context, item capacity.Selection, minutes int) (bool, error) {
			err := m.CloseCapacitySession(ctx, principal, item, minutes)
			if errors.Is(err, ErrSessionChanged) || errors.Is(err, ErrNotFound) {
				return false, nil
			}
			return err == nil, err
		},
		Observe: observe,
	})
	m.idleCleanup.Start()
}

func (m *Manager) cleanupService() (*idlecleanup.Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.idleCleanup == nil {
		return nil, ErrManagerClosed
	}
	return m.idleCleanup, nil
}

func (m *Manager) CleanupPolicy(ctx context.Context) (capacity.CleanupPolicy, error) {
	service, err := m.cleanupService()
	if err != nil {
		return capacity.CleanupPolicy{}, err
	}
	return service.Read(ctx)
}

func (m *Manager) UpdateCleanupPolicy(ctx context.Context, policy capacity.CleanupPolicy, mutate capacity.MutationRunner) error {
	service, err := m.cleanupService()
	if err != nil {
		return err
	}
	return service.Update(ctx, policy, mutate)
}
