package console

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/gorilla/websocket"
)

func NewManager(db *sql.DB, openRuntime RuntimeOpener, redact func(string) string) *Manager {
	if redact == nil {
		redact = func(value string) string { return value }
	}
	return &Manager{
		db:          db,
		openRuntime: openRuntime,
		redact:      redact,
		sessions:    map[int64]*managedConsoleSession{},
		lifecycle:   map[int64]*sync.Mutex{},
	}
}

func (m *Manager) SetAuthorizer(authorize SessionAuthorizer) {
	m.mu.Lock()
	m.authorize = authorize
	m.mu.Unlock()
}

func (m *Manager) SetSessionClosedHook(hook func(SessionHandle)) {
	m.mu.Lock()
	m.sessionClosed = hook
	m.mu.Unlock()
}

func (m *Manager) List(ctx context.Context, runtimeID int64) ([]Record, error) {
	query := `
		SELECT cs.id, cs.runtime_id, cs.generation, COALESCE(t.name, ''), cs.name, cs.status, cs.transcript, cs.error, cs.cols, cs.rows, cs.created_at, cs.updated_at, cs.closed_at, cs.environment_content_hash
		FROM console_sessions cs
		LEFT JOIN connector_runtime_surfaces rs ON rs.id = cs.runtime_id
		LEFT JOIN connector_credential_profiles p ON p.id = rs.profile_id AND p.target_id = rs.target_id AND p.connector_kind = rs.connector_kind
		LEFT JOIN connector_targets t ON t.id = p.target_id AND t.connector_kind = p.connector_kind
		WHERE (? = 0 OR cs.runtime_id = ?)
			ORDER BY CASE WHEN cs.status IN ('connecting', 'connected') THEN 0 ELSE 1 END, cs.updated_at DESC, cs.created_at DESC, cs.id DESC
			LIMIT 100`
	rows, err := m.db.QueryContext(ctx, query, runtimeID, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("list console sessions: %w", err)
	}
	defer rows.Close()

	items := []Record{}
	for rows.Next() {
		item, err := scanConsoleSession(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list console sessions: %w", err)
	}
	return items, nil
}

func (m *Manager) Create(ctx context.Context, request CreateRequest) (Record, error) {
	if request.RuntimeID < 1 {
		destroyCreateEnvironment(request)
		return Record{}, fmt.Errorf("runtime_id is required")
	}
	lock := m.runtimeLifecycle(request.RuntimeID)
	lock.Lock()
	record, session, err := m.createLocked(ctx, request)
	lock.Unlock()
	if err != nil {
		return Record{}, err
	}
	return m.finishCreate(ctx, record, session, request.WaitForStart)
}

func (m *Manager) ReplaceIfCurrent(ctx context.Context, closePrincipal executionprincipal.Principal, expected SessionHandle, request CreateRequest) (Record, error) {
	if err := closePrincipal.Validate(); err != nil {
		destroyCreateEnvironment(request)
		return Record{}, err
	}
	if request.RuntimeID < 1 || (expected.Valid() && expected.RuntimeID != request.RuntimeID) {
		destroyCreateEnvironment(request)
		return Record{}, ErrSessionChanged
	}
	lock := m.runtimeLifecycle(request.RuntimeID)
	lock.Lock()

	if expected.Valid() {
		current := m.active(expected.ID)
		if current == nil || current.runtimeID != expected.RuntimeID || current.generation != expected.Generation {
			lock.Unlock()
			destroyCreateEnvironment(request)
			return Record{}, ErrSessionChanged
		}
		status, _ := current.snapshot()
		if status != "connecting" && status != "connected" {
			lock.Unlock()
			destroyCreateEnvironment(request)
			return Record{}, ErrSessionChanged
		}
		request.Cols, request.Rows = current.dimensions()
		if err := m.closeSessionLocked(ctx, closePrincipal, current); err != nil {
			lock.Unlock()
			destroyCreateEnvironment(request)
			return Record{}, err
		}
	} else if m.activeForRuntime(request.RuntimeID) != nil {
		lock.Unlock()
		destroyCreateEnvironment(request)
		return Record{}, ErrSessionChanged
	}
	request.CloseExisting = false
	record, session, err := m.createLocked(ctx, request)
	lock.Unlock()
	if err != nil {
		return Record{}, err
	}
	return m.finishCreate(ctx, record, session, request.WaitForStart)
}

func (m *Manager) ActiveRecord(ctx context.Context, runtimeID int64) (Record, error) {
	session := m.activeForRuntime(runtimeID)
	if session == nil {
		return Record{}, ErrNotFound
	}
	return m.Get(ctx, session.id)
}

func (m *Manager) createLocked(ctx context.Context, request CreateRequest) (Record, *managedConsoleSession, error) {
	if err := request.Principal.Validate(); err != nil {
		destroyCreateEnvironment(request)
		return Record{}, nil, err
	}
	if request.RuntimeID < 1 {
		destroyCreateEnvironment(request)
		return Record{}, nil, fmt.Errorf("runtime_id is required")
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		request.Name = fmt.Sprintf("session-%d", time.Now().Unix())
	}
	if request.Cols < 1 {
		request.Cols = 120
	}
	if request.Rows < 1 {
		request.Rows = 32
	}
	if request.CloseExisting {
		if err := m.closeRuntimeLocked(ctx, request.Principal, request.RuntimeID); err != nil {
			destroyCreateEnvironment(request)
			return Record{}, nil, err
		}
	}
	if !request.CloseExisting && m.activeSessionCount() >= maxActiveConsoleSessions {
		destroyCreateEnvironment(request)
		return Record{}, nil, ErrSessionLimit
	}

	var err error
	now := time.Now().UTC().Format(time.RFC3339)
	m.mu.Lock()
	var generation int64
	err = m.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation), 0) + 1 FROM console_sessions WHERE runtime_id = ?`, request.RuntimeID).Scan(&generation)
	if err != nil {
		m.mu.Unlock()
		request.Environment.Destroy()
		return Record{}, nil, fmt.Errorf("read next console generation: %w", err)
	}
	result, err := m.db.ExecContext(ctx, `
		INSERT INTO console_sessions (
			runtime_id, generation, principal_kind, principal_token_id, workspace_id,
			runtime_instance_id, environment_content_hash, approval_context_hash,
			name, status, cols, rows, created_at, updated_at
		)
		VALUES (?, ?, ?, NULLIF(?, 0), ?, ?, ?, ?, ?, 'connecting', ?, ?, ?, ?)`,
		request.RuntimeID,
		generation,
		string(request.Principal.Kind),
		request.Principal.TokenID,
		request.Principal.WorkspaceID,
		request.Principal.RuntimeInstanceID,
		strings.TrimSpace(request.EnvironmentContentHash),
		strings.TrimSpace(request.ApprovalContextHash),
		request.Name,
		request.Cols,
		request.Rows,
		now,
		now,
	)
	if err != nil {
		m.mu.Unlock()
		request.Environment.Destroy()
		return Record{}, nil, fmt.Errorf("create console session: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		m.mu.Unlock()
		request.Environment.Destroy()
		return Record{}, nil, fmt.Errorf("read console session id: %w", err)
	}

	sessionCtx, cancel := context.WithCancel(context.Background())
	managed := &managedConsoleSession{
		id:                     id,
		runtimeID:              request.RuntimeID,
		generation:             generation,
		name:                   request.Name,
		cols:                   request.Cols,
		rows:                   request.Rows,
		params:                 cloneParams(request.Params),
		principal:              request.Principal,
		environment:            request.Environment,
		prepareEnvironment:     request.PrepareEnvironment,
		environmentContentHash: strings.TrimSpace(request.EnvironmentContentHash),
		approvalContextHash:    strings.TrimSpace(request.ApprovalContextHash),
		manager:                m,
		ctx:                    sessionCtx,
		cancel:                 cancel,
		start:                  make(chan struct{}),
		done:                   make(chan struct{}),
		status:                 "connecting",
		clients:                map[*websocket.Conn]*sync.Mutex{},
	}

	m.sessions[id] = managed
	m.mu.Unlock()
	go managed.run()
	record, err := m.Get(ctx, id)
	if err != nil {
		managed.close()
		return Record{}, nil, err
	}
	return record, managed, nil
}

func (m *Manager) finishCreate(ctx context.Context, record Record, session *managedConsoleSession, waitForStart bool) (Record, error) {
	if !waitForStart {
		return record, nil
	}
	if session == nil {
		return Record{}, errors.New("console session did not start")
	}
	if err := session.waitStart(ctx); err != nil {
		session.close()
		_ = session.waitDone(ctx)
		return Record{}, err
	}
	return m.Get(ctx, record.ID)
}

func destroyCreateEnvironment(request CreateRequest) {
	if request.Environment != nil {
		request.Environment.Destroy()
	}
}

func (m *Manager) Get(ctx context.Context, id int64) (Record, error) {
	row := m.db.QueryRowContext(ctx, `
		SELECT cs.id, cs.runtime_id, cs.generation, COALESCE(t.name, ''), cs.name, cs.status, cs.transcript, cs.error, cs.cols, cs.rows, cs.created_at, cs.updated_at, cs.closed_at, cs.environment_content_hash
		FROM console_sessions cs
		LEFT JOIN connector_runtime_surfaces rs ON rs.id = cs.runtime_id
		LEFT JOIN connector_credential_profiles p ON p.id = rs.profile_id AND p.target_id = rs.target_id AND p.connector_kind = rs.connector_kind
		LEFT JOIN connector_targets t ON t.id = p.target_id AND t.connector_kind = p.connector_kind
		WHERE cs.id = ?`, id)
	record, err := scanConsoleSession(row)
	if err != nil {
		return Record{}, err
	}
	record.Transcript = m.transcriptTail(ctx, id, maxConsoleTranscriptLength, record.Transcript)
	return record, nil
}

func (m *Manager) ActiveSnapshot(ctx context.Context, principal executionprincipal.Principal, runtimeID int64) (Record, error) {
	session := m.activeForRuntime(runtimeID)
	if session == nil {
		return Record{}, ErrNotFound
	}
	var record Record
	err := m.authorizeOperation(ctx, principal, session, OperationObserve, func() error {
		var getErr error
		record, getErr = m.Get(ctx, session.id)
		return getErr
	})
	return record, err
}

func (m *Manager) transcriptTail(ctx context.Context, sessionID int64, limit int, fallback string) string {
	if m == nil || m.db == nil || sessionID < 1 {
		return TailStringByBytes(fallback, limit)
	}
	if limit < 1 {
		limit = maxConsoleTranscriptLength
	}
	rows, err := m.db.QueryContext(ctx, `
		SELECT data
		FROM console_session_chunks
		WHERE session_id = ?
		ORDER BY seq DESC
		LIMIT ?`,
		sessionID,
		(limit/maxConsoleChunkLength)+2,
	)
	if err != nil {
		return TailStringByBytes(fallback, limit)
	}
	defer rows.Close()

	chunks := []string{}
	total := 0
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return TailStringByBytes(fallback, limit)
		}
		if data == "" {
			continue
		}
		chunks = append(chunks, data)
		total += len(data)
		if total >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil || len(chunks) == 0 {
		return TailStringByBytes(fallback, limit)
	}

	var builder strings.Builder
	for index := len(chunks) - 1; index >= 0; index-- {
		builder.WriteString(chunks[index])
	}
	return TailStringByBytes(builder.String(), limit)
}

func (m *Manager) Input(ctx context.Context, principal executionprincipal.Principal, id int64, data string) error {
	if data == "" {
		return nil
	}
	if len(data) > maxConsoleInputBytes {
		return ErrInputTooLarge
	}
	session := m.active(id)
	if session == nil {
		return fmt.Errorf("console session is not active")
	}
	return m.authorizeOperation(ctx, principal, session, OperationInput, func() error {
		manualCommands := session.prepareManualInput(data)
		if err := session.writeInput(data); err != nil {
			return err
		}
		session.persistManualInput(manualCommands)
		return nil
	})
}

func (m *Manager) Exec(ctx context.Context, principal executionprincipal.Principal, runtimeID int64, command string) (ExecResult, error) {
	if err := principal.Validate(); err != nil {
		return ExecResult{}, err
	}
	session := m.activeForRuntime(runtimeID)
	if session == nil {
		record, err := m.Create(ctx, CreateRequest{
			RuntimeID: runtimeID,
			Name:      fmt.Sprintf("runtime-%d ai session", runtimeID),
			Cols:      120,
			Rows:      32,
			Principal: principal,
		})
		if err != nil {
			return ExecResult{}, err
		}
		session = m.active(record.ID)
	}
	if session == nil {
		return ExecResult{}, fmt.Errorf("console session did not start")
	}
	result, execErr := session.execCommand(ctx, command, func(write func() error) error {
		return m.authorizeOperation(ctx, principal, session, OperationExecute, write)
	})
	if execErr != nil && !errors.Is(execErr, ErrCommandActive) {
		return result, execErr
	}
	if err := m.authorizeOperation(ctx, principal, session, OperationObserve, nil); err != nil {
		return ExecResult{}, err
	}
	return result, execErr
}

func (m *Manager) EnsureReady(ctx context.Context, principal executionprincipal.Principal, runtimeID int64) (SessionHandle, error) {
	if err := principal.Validate(); err != nil {
		return SessionHandle{}, err
	}
	session := m.activeForRuntime(runtimeID)
	if session == nil {
		record, err := m.Create(ctx, CreateRequest{
			RuntimeID: runtimeID,
			Name:      fmt.Sprintf("runtime-%d ai session", runtimeID),
			Cols:      120,
			Rows:      32,
			Principal: principal,
		})
		if err != nil {
			return SessionHandle{}, err
		}
		session = m.active(record.ID)
	}
	if session == nil {
		return SessionHandle{}, fmt.Errorf("console session did not start")
	}
	if err := m.authorizeOperation(ctx, principal, session, OperationObserve, nil); err != nil {
		return SessionHandle{}, err
	}
	if err := session.waitReady(ctx); err != nil {
		return session.handle(), err
	}
	return session.handle(), nil
}

func (m *Manager) WaitActive(ctx context.Context, principal executionprincipal.Principal, handle SessionHandle) (ExecResult, error) {
	session, err := m.exactSession(handle)
	if err != nil {
		return ExecResult{}, err
	}
	if err := m.authorizeOperation(ctx, principal, session, OperationObserve, nil); err != nil {
		return ExecResult{}, err
	}
	result, waitErr := session.waitActiveCommand(ctx)
	if waitErr != nil {
		return result, waitErr
	}
	if err := m.authorizeOperation(ctx, principal, session, OperationObserve, nil); err != nil {
		return ExecResult{}, err
	}
	return result, nil
}

func (m *Manager) InterruptActive(ctx context.Context, principal executionprincipal.Principal, handle SessionHandle) error {
	session, err := m.exactSession(handle)
	if err != nil {
		return err
	}
	return m.authorizeOperation(ctx, principal, session, OperationInterrupt, func() error {
		return session.interruptActiveCommand(ctx)
	})
}

func (m *Manager) Resize(id int64, cols int, rows int) {
	session := m.active(id)
	if session == nil || cols < 1 || rows < 1 {
		return
	}
	lock := m.runtimeLifecycle(session.runtimeID)
	lock.Lock()
	defer lock.Unlock()
	session = m.active(id)
	if session == nil {
		return
	}
	session.resize(cols, rows)
}

func (m *Manager) Close(ctx context.Context, principal executionprincipal.Principal, id int64) error {
	session := m.active(id)
	if session != nil {
		lock := m.runtimeLifecycle(session.runtimeID)
		lock.Lock()
		defer lock.Unlock()
		session = m.active(id)
		if session == nil {
			return nil
		}
		return m.authorizeOperation(ctx, principal, session, OperationClose, func() error {
			session.close()
			return nil
		})
	}
	if err := principal.Validate(); err != nil || !principal.IsLocalOperator() {
		return ErrUnauthorized
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := m.db.ExecContext(ctx, `UPDATE console_sessions SET status = 'closed', closed_at = COALESCE(closed_at, ?), updated_at = ? WHERE id = ?`, now, now, id)
	return err
}

func (m *Manager) CloseRuntime(ctx context.Context, principal executionprincipal.Principal, runtimeID int64) error {
	if err := principal.Validate(); err != nil {
		return err
	}
	lock := m.runtimeLifecycle(runtimeID)
	lock.Lock()
	defer lock.Unlock()
	return m.closeRuntimeLocked(ctx, principal, runtimeID)
}

func (m *Manager) closeRuntimeLocked(ctx context.Context, principal executionprincipal.Principal, runtimeID int64) error {
	m.mu.Lock()
	sessions := []*managedConsoleSession{}
	for _, session := range m.sessions {
		if session.runtimeID == runtimeID {
			sessions = append(sessions, session)
		}
	}
	m.mu.Unlock()
	for _, session := range sessions {
		if err := m.authorizeOperation(ctx, principal, session, OperationClose, nil); err != nil {
			return err
		}
	}
	for _, session := range sessions {
		session.close()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := m.db.ExecContext(ctx, `UPDATE console_sessions SET status = 'closed', closed_at = COALESCE(closed_at, ?), updated_at = ? WHERE runtime_id = ? AND status IN ('connecting', 'connected')`, now, now, runtimeID)
	return err
}

func (m *Manager) closeSessionLocked(ctx context.Context, principal executionprincipal.Principal, session *managedConsoleSession) error {
	if session == nil {
		return ErrNotFound
	}
	return m.authorizeOperation(ctx, principal, session, OperationClose, func() error {
		session.mu.Lock()
		session.status = "closed"
		session.mu.Unlock()
		session.close()
		now := time.Now().UTC().Format(time.RFC3339)
		_, err := m.db.ExecContext(ctx, `
			UPDATE console_sessions
			SET status = 'closed', closed_at = COALESCE(closed_at, ?), updated_at = ?
			WHERE id = ? AND generation = ? AND status IN ('connecting', 'connected')`,
			now, now, session.id, session.generation,
		)
		return err
	})
}

func (m *Manager) runtimeLifecycle(runtimeID int64) *sync.Mutex {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.lifecycle == nil {
		m.lifecycle = make(map[int64]*sync.Mutex)
	}
	lock := m.lifecycle[runtimeID]
	if lock == nil {
		lock = &sync.Mutex{}
		m.lifecycle[runtimeID] = lock
	}
	return lock
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := make([]*managedConsoleSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.Unlock()
	for _, session := range sessions {
		session.close()
	}
}

func (m *Manager) Attach(w http.ResponseWriter, r *http.Request, principal executionprincipal.Principal, id int64, upgrade func(http.ResponseWriter, *http.Request) (*websocket.Conn, error)) error {
	session := m.active(id)
	if session == nil {
		record, err := m.Get(r.Context(), id)
		if err != nil {
			return ErrNotFound
		}
		return InactiveError{Status: record.Status, Detail: record.Error}
	}
	if err := m.authorizeOperation(r.Context(), principal, session, OperationAttach, nil); err != nil {
		return err
	}

	ws, err := upgrade(w, r)
	if err != nil {
		return err
	}
	defer ws.Close()
	ws.SetReadLimit(maxPTYClientMessageBytes)
	_ = ws.SetReadDeadline(time.Now().Add(ptyPongWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(ptyPongWait))
	})

	writeMu, err := session.addClient(ws)
	if err != nil {
		return err
	}
	defer session.removeClient(ws)
	stopPing := make(chan struct{})
	defer close(stopPing)
	go keepPTYAlive(ws, writeMu, stopPing)

	if err := m.authorizeOperation(r.Context(), principal, session, OperationObserve, func() error {
		snapshotStatus, transcript := session.snapshot()
		return writePTYMessage(ws, writeMu, ptyServerMessage{
			Type: "snapshot", Status: snapshotStatus, Data: transcript, SessionID: session.id,
		})
	}); err != nil {
		return err
	}

	inputLimiter := newConsoleIntervalLimiter(ptyInputMinInterval)
	resizeLimiter := newConsoleIntervalLimiter(ptyResizeMinInterval)
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			return nil
		}
		var message ptyClientMessage
		if err := json.Unmarshal(data, &message); err != nil {
			continue
		}
		switch message.Type {
		case "input":
			if len(message.Data) > maxConsoleInputBytes {
				_ = writePTYMessage(ws, writeMu, ptyServerMessage{Type: "error", Status: "error", Data: ErrInputTooLarge.Error(), SessionID: session.id})
				continue
			}
			if !inputLimiter.allow() {
				continue
			}
			if err := m.authorizeOperation(r.Context(), principal, session, OperationInput, func() error {
				manualCommands := session.prepareManualInput(message.Data)
				if err := session.writeInput(message.Data); err != nil {
					return err
				}
				session.persistManualInput(manualCommands)
				return nil
			}); err != nil {
				_ = writePTYMessage(ws, writeMu, ptyServerMessage{Type: "error", Status: "error", Data: err.Error(), SessionID: session.id})
			}
		case "resize":
			if !resizeLimiter.allow() {
				continue
			}
			_ = m.authorizeOperation(r.Context(), principal, session, OperationInput, func() error {
				session.resize(message.Cols, message.Rows)
				return nil
			})
		}
	}
}

func (m *Manager) authorizeOperation(
	ctx context.Context,
	principal executionprincipal.Principal,
	session *managedConsoleSession,
	operation SessionOperation,
	run func() error,
) error {
	if run == nil {
		run = func() error { return nil }
	}
	if session == nil || principal.Validate() != nil || !principal.SameRuntime(session.principal) {
		return ErrUnauthorized
	}
	if principal.IsLocalOperator() || session.environmentContentHash == "" {
		return run()
	}
	m.mu.Lock()
	authorize := m.authorize
	m.mu.Unlock()
	if authorize == nil {
		return ErrUnauthorized
	}
	return authorize(ctx, principal, SessionAuthorization{
		Handle:                 session.handle(),
		EnvironmentContentHash: session.environmentContentHash,
		ApprovalContextHash:    session.approvalContextHash,
	}, operation, run)
}

func (m *Manager) exactSession(handle SessionHandle) (*managedConsoleSession, error) {
	if !handle.Valid() {
		return nil, ErrNotFound
	}
	session := m.active(handle.ID)
	if session == nil || session.runtimeID != handle.RuntimeID || session.generation != handle.Generation {
		return nil, ErrNotFound
	}
	return session, nil
}

func (m *Manager) activeSessionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, session := range m.sessions {
		status, _ := session.snapshot()
		if status == "connecting" || status == "connected" {
			count++
		}
	}
	return count
}

func (m *Manager) active(id int64) *managedConsoleSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (m *Manager) activeForRuntime(runtimeID int64) *managedConsoleSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	var selected *managedConsoleSession
	for _, session := range m.sessions {
		if session.runtimeID == runtimeID {
			status, _ := session.snapshot()
			if status == "connecting" || status == "connected" {
				if selected == nil || session.id > selected.id {
					selected = session
				}
			}
		}
	}
	return selected
}

func (m *Manager) remove(id int64) {
	m.mu.Lock()
	session := m.sessions[id]
	delete(m.sessions, id)
	hook := m.sessionClosed
	m.mu.Unlock()
	if session != nil && hook != nil {
		hook(session.handle())
	}
}

func (s *managedConsoleSession) handle() SessionHandle {
	if s == nil {
		return SessionHandle{}
	}
	return SessionHandle{ID: s.id, RuntimeID: s.runtimeID, Generation: s.generation}
}

func (m *Manager) SeedActiveCommandForTest(id int64, runtimeID int64, command string, output string) {
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = &managedConsoleSession{
		id:            id,
		runtimeID:     runtimeID,
		generation:    id,
		ctx:           sessionCtx,
		cancel:        sessionCancel,
		status:        "connected",
		rawTranscript: output,
		activeExec: &consoleSessionActiveExec{
			Command:     command,
			Marker:      "__AIPERMISSION_EXIT_ACTIVE__",
			StartOffset: 0,
			Started:     time.Now().Add(-time.Second),
		},
	}
}
