package console

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

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
	}
}

func (m *Manager) List(ctx context.Context, runtimeID int64) ([]Record, error) {
	query := `
		SELECT cs.id, cs.runtime_id, COALESCE(t.name, ''), cs.name, cs.status, cs.transcript, cs.error, cs.cols, cs.rows, cs.created_at, cs.updated_at, cs.closed_at
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
		return Record{}, fmt.Errorf("runtime_id is required")
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
		_ = m.CloseRuntime(ctx, request.RuntimeID)
	}
	if !request.CloseExisting && m.activeSessionCount() >= maxActiveConsoleSessions {
		return Record{}, ErrSessionLimit
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := m.db.ExecContext(ctx, `
		INSERT INTO console_sessions (runtime_id, name, status, cols, rows, created_at, updated_at)
		VALUES (?, ?, 'connecting', ?, ?, ?, ?)`,
		request.RuntimeID,
		request.Name,
		request.Cols,
		request.Rows,
		now,
		now,
	)
	if err != nil {
		return Record{}, fmt.Errorf("create console session: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Record{}, fmt.Errorf("read console session id: %w", err)
	}

	sessionCtx, cancel := context.WithCancel(context.Background())
	managed := &managedConsoleSession{
		id:        id,
		runtimeID: request.RuntimeID,
		name:      request.Name,
		cols:      request.Cols,
		rows:      request.Rows,
		manager:   m,
		ctx:       sessionCtx,
		cancel:    cancel,
		start:     make(chan struct{}),
		done:      make(chan struct{}),
		status:    "connecting",
		clients:   map[*websocket.Conn]*sync.Mutex{},
	}

	m.mu.Lock()
	m.sessions[id] = managed
	m.mu.Unlock()
	go managed.run()
	if request.WaitForStart {
		if err := managed.waitStart(ctx); err != nil {
			managed.close()
			_ = managed.waitDone(ctx)
			return Record{}, err
		}
	}

	return m.Get(ctx, id)
}

func (m *Manager) Get(ctx context.Context, id int64) (Record, error) {
	row := m.db.QueryRowContext(ctx, `
		SELECT cs.id, cs.runtime_id, COALESCE(t.name, ''), cs.name, cs.status, cs.transcript, cs.error, cs.cols, cs.rows, cs.created_at, cs.updated_at, cs.closed_at
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

func (m *Manager) Input(ctx context.Context, id int64, data string) error {
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
	manualCommands := session.prepareManualInput(data)
	if err := session.writeInput(data); err != nil {
		return err
	}
	session.persistManualInput(manualCommands)
	return nil
}

func (m *Manager) Exec(ctx context.Context, runtimeID int64, command string) (ExecResult, error) {
	session := m.activeForRuntime(runtimeID)
	if session == nil {
		record, err := m.Create(ctx, CreateRequest{
			RuntimeID: runtimeID,
			Name:      fmt.Sprintf("runtime-%d ai session", runtimeID),
			Cols:      120,
			Rows:      32,
		})
		if err != nil {
			return ExecResult{}, err
		}
		session = m.active(record.ID)
	}
	if session == nil {
		return ExecResult{}, fmt.Errorf("console session did not start")
	}
	return session.execCommand(ctx, command)
}

func (m *Manager) EnsureReady(ctx context.Context, runtimeID int64) (int64, error) {
	session := m.activeForRuntime(runtimeID)
	if session == nil {
		record, err := m.Create(ctx, CreateRequest{
			RuntimeID: runtimeID,
			Name:      fmt.Sprintf("runtime-%d ai session", runtimeID),
			Cols:      120,
			Rows:      32,
		})
		if err != nil {
			return 0, err
		}
		session = m.active(record.ID)
	}
	if session == nil {
		return 0, fmt.Errorf("console session did not start")
	}
	if err := session.waitReady(ctx); err != nil {
		return session.id, err
	}
	return session.id, nil
}

func (m *Manager) WaitActive(ctx context.Context, runtimeID int64) (ExecResult, error) {
	session := m.activeForRuntime(runtimeID)
	if session == nil {
		return ExecResult{}, fmt.Errorf("console session is not active")
	}
	return session.waitActiveCommand(ctx)
}

func (m *Manager) InterruptActive(ctx context.Context, runtimeID int64) error {
	session := m.activeForRuntime(runtimeID)
	if session == nil {
		return nil
	}
	return session.interruptActiveCommand(ctx)
}

func (m *Manager) Resize(id int64, cols int, rows int) {
	session := m.active(id)
	if session == nil || cols < 1 || rows < 1 {
		return
	}
	session.resize(cols, rows)
}

func (m *Manager) Close(ctx context.Context, id int64) error {
	session := m.active(id)
	if session != nil {
		session.close()
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := m.db.ExecContext(ctx, `UPDATE console_sessions SET status = 'closed', closed_at = COALESCE(closed_at, ?), updated_at = ? WHERE id = ?`, now, now, id)
	return err
}

func (m *Manager) CloseRuntime(ctx context.Context, runtimeID int64) error {
	m.mu.Lock()
	sessions := []*managedConsoleSession{}
	for _, session := range m.sessions {
		if session.runtimeID == runtimeID {
			sessions = append(sessions, session)
		}
	}
	m.mu.Unlock()
	for _, session := range sessions {
		session.close()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := m.db.ExecContext(ctx, `UPDATE console_sessions SET status = 'closed', closed_at = COALESCE(closed_at, ?), updated_at = ? WHERE runtime_id = ? AND status IN ('connecting', 'connected')`, now, now, runtimeID)
	return err
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

func (m *Manager) Attach(w http.ResponseWriter, r *http.Request, id int64, upgrade func(http.ResponseWriter, *http.Request) (*websocket.Conn, error)) error {
	session := m.active(id)
	if session == nil {
		record, err := m.Get(r.Context(), id)
		if err != nil {
			return ErrNotFound
		}
		return InactiveError{Status: record.Status, Detail: record.Error}
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

	snapshotStatus, transcript := session.snapshot()
	_ = writePTYMessage(ws, writeMu, ptyServerMessage{Type: "snapshot", Status: snapshotStatus, Data: transcript, SessionID: session.id})

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
			manualCommands := session.prepareManualInput(message.Data)
			if err := session.writeInput(message.Data); err == nil {
				session.persistManualInput(manualCommands)
			}
		case "resize":
			if !resizeLimiter.allow() {
				continue
			}
			session.resize(message.Cols, message.Rows)
		}
	}
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
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

func (m *Manager) SeedActiveCommandForTest(id int64, runtimeID int64, command string, output string) {
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = &managedConsoleSession{
		id:            id,
		runtimeID:     runtimeID,
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
