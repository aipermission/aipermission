package console

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console/capacity"
	"github.com/aipermission/aipermission/backend/internal/console/terminaltext"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

// Notifications carry no output and coalesce naturally: each subscriber only
// needs the newest bounded snapshot, not every terminal output chunk.
func (m *Manager) capacityChanged() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capacityRevision++
	if m.capacitySignal != nil {
		close(m.capacitySignal)
	}
	m.capacitySignal = make(chan struct{})
}

func (m *Manager) WaitCapacity(ctx context.Context, after uint64) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	if m.capacityRevision == 0 {
		m.capacityRevision = 1
	}
	if m.capacitySignal == nil {
		m.capacitySignal = make(chan struct{})
	}
	signal, revision, closed := m.capacitySignal, m.capacityRevision, m.closed
	m.mu.Unlock()
	if closed {
		return 0, ErrManagerClosed
	}
	if after > 0 && after == revision {
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		select {
		case <-signal:
		case <-timer.C:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, ErrManagerClosed
	}
	return m.capacityRevision, nil
}

func (m *Manager) Capacity(ctx context.Context) (capacity.Report, error) {
	if _, err := m.WaitCapacity(ctx, 0); err != nil {
		return capacity.Report{}, err
	}
	m.mu.Lock()
	items := make([]*managedConsoleSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		items = append(items, session)
	}
	revision := m.capacityRevision
	m.mu.Unlock()
	report := capacity.Report{Limit: maxActiveConsoleSessions, Used: len(items), Available: maxActiveConsoleSessions - len(items), Revision: revision, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Items: []capacity.Session{}}
	for _, session := range items {
		report.Items = append(report.Items, session.capacitySnapshot())
	}
	return report, m.capacityMetadata(ctx, report.Items)
}

func (m *Manager) capacityMetadata(ctx context.Context, items []capacity.Session) error {
	if len(items) == 0 {
		return nil
	}
	placeholders, args := make([]string, len(items)), make([]any, len(items))
	for i, item := range items {
		placeholders[i], args[i] = "?", item.RuntimeID
	}
	rows, err := m.db.QueryContext(ctx, `SELECT rs.id, COALESCE(t.name, ''), rs.connector_kind FROM connector_runtime_surfaces rs LEFT JOIN connector_targets t ON t.id = rs.target_id AND t.connector_kind = rs.connector_kind WHERE rs.id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return fmt.Errorf("read console capacity metadata: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, kind string
		if err := rows.Scan(&id, &name, &kind); err != nil {
			return err
		}
		for i := range items {
			if items[i].RuntimeID == id {
				items[i].TargetName, items[i].ConnectorKind = terminaltext.TailStringByBytes(name, 256), kind
			}
		}
	}
	return rows.Err()
}

func (s *managedConsoleSession) capacitySnapshot() capacity.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capacitySnapshotLocked()
}

func (s *managedConsoleSession) capacitySnapshotLocked() capacity.Session {
	state := s.status
	canClose := state == "connected" && !s.closing && s.activeExec == nil && s.manualActive == nil && s.manualPause == nil && !s.cleanupUntracked && !s.manualInput.Pending()
	if s.activeExec != nil || s.manualActive != nil {
		state = "running"
	} else if state == "connected" && !canClose {
		state = "untracked"
	}
	return capacity.Session{ID: s.id, RuntimeID: s.runtimeID, Generation: s.generation, Name: terminaltext.TailStringByBytes(s.name, 256), Owner: s.principal.String(), State: state, Clients: len(s.clients), LastActivity: s.lastActivity.UTC().Format(time.RFC3339Nano), CanClose: canClose}
}

func (m *Manager) CloseCapacitySession(ctx context.Context, principal executionprincipal.Principal, selection capacity.Selection, minutes int) error {
	if !principal.IsLocalOperator() {
		return ErrUnauthorized
	}
	if !capacity.ValidIdleMinutes(minutes) || selection.ID < 1 || selection.Generation < 1 {
		return ErrSessionChanged
	}
	session := m.active(selection.ID)
	if session == nil {
		return ErrNotFound
	}
	lock := m.runtimeLifecycle(session.runtimeID)
	lock.Lock()
	defer lock.Unlock()
	if m.active(selection.ID) != session {
		return ErrSessionChanged
	}
	err := m.authorizeOperation(ctx, principal, session, OperationClose, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !session.inputMu.TryLock() {
			return ErrSessionChanged
		}
		defer session.inputMu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		session.mu.Lock()
		item := session.capacitySnapshotLocked()
		if item.Generation != selection.Generation || item.LastActivity != selection.LastActivity || !item.Eligible(time.Now(), minutes) {
			session.mu.Unlock()
			return ErrSessionChanged
		}
		// Claim closing under the same mutex used by input, command admission and
		// client attachment, so a fresh command cannot slip past this check.
		session.closing, session.status = true, "closing"
		session.mu.Unlock()
		session.beginClose()
		return nil
	})
	if err != nil {
		return err
	}
	return session.waitDone(ctx)
}
