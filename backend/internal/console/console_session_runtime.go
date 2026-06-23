package console

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

func (s *managedConsoleSession) run() {
	defer func() {
		s.manager.remove(s.id)
		if s.done != nil {
			close(s.done)
		}
	}()

	if s.manager.openRuntime == nil {
		s.markStarted(fmt.Errorf("console transport is not configured"))
		s.fail("console transport is not configured")
		return
	}
	runtime, err := s.manager.openRuntime(s.ctx, s.runtimeID, s.rows, s.cols, cloneParams(s.params))
	if err != nil {
		s.markStarted(err)
		s.fail(err.Error())
		return
	}
	s.mu.Lock()
	s.runtime = runtime
	s.mu.Unlock()
	defer runtime.close()

	stdin := runtime.Stdin
	if stdin == nil {
		s.markStarted(fmt.Errorf("console transport did not provide stdin"))
		s.fail("console transport did not provide stdin")
		return
	}
	s.mu.Lock()
	s.stdin = stdin
	s.mu.Unlock()

	if runtime.Stdout == nil {
		s.markStarted(fmt.Errorf("console transport did not provide stdout"))
		s.fail("console transport did not provide stdout")
		return
	}
	if runtime.Wait == nil {
		s.markStarted(fmt.Errorf("console transport did not provide wait"))
		s.fail("console transport did not provide wait")
		return
	}

	s.setStatus("connected", "")
	s.broadcast(ptyServerMessage{Type: "ready", Status: "connected", SessionID: s.id})
	s.markStarted(nil)

	if runtime.StartupInputAfterConnect != "" {
		if _, err := io.WriteString(stdin, runtime.StartupInputAfterConnect); err != nil {
			s.fail(fmt.Sprintf("write startup input: %v", err))
			return
		}
	}

	go s.pipe(runtime.Stdout)
	if runtime.Stderr != nil {
		go s.pipe(runtime.Stderr)
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- runtime.Wait()
	}()

	select {
	case err := <-waitDone:
		if err != nil && !errors.Is(err, io.EOF) {
			s.finish("closed", err.Error())
			return
		}
		s.finish("closed", "")
	case <-s.ctx.Done():
		s.finish("closed", "")
	}
}

func (s *managedConsoleSession) pipe(reader io.Reader) {
	buffer := make([]byte, 4096)
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			s.appendOutput(string(buffer[:n]))
		}
		if err != nil {
			return
		}
	}
}

func (s *managedConsoleSession) addClient(ws *websocket.Conn) (*sync.Mutex, error) {
	writeMu := &sync.Mutex{}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.clients) >= maxConsoleClientsPerSession {
		return nil, ErrClientLimit
	}
	s.clients[ws] = writeMu
	return writeMu, nil
}

func (s *managedConsoleSession) removeClient(ws *websocket.Conn) {
	s.mu.Lock()
	delete(s.clients, ws)
	s.mu.Unlock()
}

func (s *managedConsoleSession) snapshot() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.transcript
}

func (s *managedConsoleSession) writeInput(data string) error {
	if data == "" {
		return nil
	}
	s.mu.Lock()
	stdin := s.stdin
	s.mu.Unlock()
	if stdin == nil {
		return fmt.Errorf("console session is not ready")
	}
	_, err := io.WriteString(stdin, data)
	return err
}

func (s *managedConsoleSession) resize(cols int, rows int) {
	if cols < 1 || rows < 1 {
		return
	}
	s.mu.Lock()
	s.cols = cols
	s.rows = rows
	runtime := s.runtime
	s.mu.Unlock()
	if runtime != nil && runtime.Resize != nil {
		_ = runtime.Resize(cols, rows)
	}
	if _, err := s.manager.db.Exec(`UPDATE console_sessions SET cols = ?, rows = ?, updated_at = ? WHERE id = ?`, cols, rows, time.Now().UTC().Format(time.RFC3339), s.id); err != nil {
		logConsolePersistError("resize", s.id, err)
	}
}

func (s *managedConsoleSession) close() {
	s.cancel()
	s.mu.Lock()
	runtime := s.runtime
	s.mu.Unlock()
	if runtime != nil {
		_ = runtime.close()
	}
}

func (session *RuntimeSession) close() error {
	if session == nil || session.Close == nil {
		return nil
	}
	return session.Close()
}

func (s *managedConsoleSession) fail(message string) {
	s.setStatus("error", message)
	s.broadcast(ptyServerMessage{Type: "error", Status: "error", Data: message, SessionID: s.id})
	s.finish("error", message)
}

func (s *managedConsoleSession) finish(status string, message string) {
	now := time.Now().UTC().Format(time.RFC3339)
	persistedMessage := s.manager.redactText(message)
	s.closeManualOutputCapture(manualSessionClosed)
	s.mu.Lock()
	s.status = status
	if persistedMessage != "" {
		s.errText = persistedMessage
	}
	s.mu.Unlock()
	s.flushTranscript()
	if _, err := s.manager.db.Exec(`UPDATE console_sessions SET status = ?, error = ?, closed_at = COALESCE(closed_at, ?), updated_at = ? WHERE id = ?`, status, persistedMessage, now, now, s.id); err != nil {
		logConsolePersistError("finish", s.id, err)
	}
	s.broadcast(ptyServerMessage{Type: "exit", Status: status, Data: persistedMessage, SessionID: s.id})
}

func (s *managedConsoleSession) markStarted(err error) {
	s.startOnce.Do(func() {
		s.mu.Lock()
		s.startErr = err
		s.mu.Unlock()
		close(s.start)
	})
}

func (s *managedConsoleSession) waitStart(ctx context.Context) error {
	if s == nil || s.start == nil {
		return nil
	}
	select {
	case <-s.start:
		s.mu.Lock()
		err := s.startErr
		s.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *managedConsoleSession) waitDone(ctx context.Context) error {
	if s == nil || s.done == nil {
		return nil
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *managedConsoleSession) setStatus(status string, message string) {
	now := time.Now().UTC().Format(time.RFC3339)
	persistedMessage := s.manager.redactText(message)
	s.mu.Lock()
	s.status = status
	s.errText = persistedMessage
	s.mu.Unlock()
	if _, err := s.manager.db.Exec(`UPDATE console_sessions SET status = ?, error = ?, updated_at = ? WHERE id = ?`, status, persistedMessage, now, s.id); err != nil {
		logConsolePersistError("set_status", s.id, err)
	}
}

func (s *managedConsoleSession) appendOutput(data string) {
	if data == "" {
		return
	}
	s.mu.Lock()
	automationActive := s.activeExec != nil
	postAutomationFilter := !automationActive && time.Now().Before(s.filterUntil)
	keepShellPrompt := postAutomationFilter
	s.rawTranscript = limitConsoleTranscript(s.rawTranscript + data)
	if automationActive {
		active := s.activeExec
		startOffset := active.StartOffset
		if startOffset > len(s.rawTranscript) {
			startOffset = 0
		}
		if strings.Contains(s.rawTranscript[startOffset:], "\n"+active.Marker+":") {
			keepShellPrompt = true
		}
	}
	displayData := data
	if automationActive || postAutomationFilter {
		displayData = cleanConsoleDisplayOutput(data, keepShellPrompt)
	}
	if displayData != "" {
		s.transcript = limitConsoleTranscript(s.transcript + displayData)
		s.pendingOutput += displayData
	}
	flushSoon := len(s.pendingOutput) >= maxConsolePendingFlushSize
	if s.manager != nil && s.manager.db != nil && s.persistTimer == nil {
		s.persistTimer = time.AfterFunc(500*time.Millisecond, s.flushTranscript)
	}
	manualCompletion := s.manualOutputCompletionLocked()
	s.clearManualPauseIfPromptReturnedLocked()
	s.mu.Unlock()
	if manualCompletion != nil {
		go s.finishManualOutputCapture(manualCompletion)
	}
	if flushSoon {
		go s.flushTranscript()
	}
	if displayData != "" {
		s.broadcast(ptyServerMessage{Type: "output", Status: "connected", Data: displayData, SessionID: s.id})
	}
}

func (s *managedConsoleSession) appendDisplayOutput(data string) {
	if data == "" {
		return
	}
	s.mu.Lock()
	if strings.HasPrefix(data, "[AI command]") && s.transcript != "" && !strings.HasSuffix(s.transcript, "\n") && !strings.HasSuffix(s.transcript, "\r") {
		data = "\r\n" + data
	}
	s.transcript = limitConsoleTranscript(s.transcript + data)
	s.pendingOutput += data
	flushSoon := len(s.pendingOutput) >= maxConsolePendingFlushSize
	if s.manager != nil && s.manager.db != nil && s.persistTimer == nil {
		s.persistTimer = time.AfterFunc(500*time.Millisecond, s.flushTranscript)
	}
	s.mu.Unlock()
	if flushSoon {
		go s.flushTranscript()
	}
	s.broadcast(ptyServerMessage{Type: "output", Status: "connected", Data: data, SessionID: s.id})
}

func (s *managedConsoleSession) flushTranscript() {
	if s.manager == nil || s.manager.db == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	if s.persistTimer != nil {
		s.persistTimer.Stop()
		s.persistTimer = nil
	}
	snapshot := TailStringByBytes(s.transcript, maxConsoleSnapshotLength)
	pending := s.pendingOutput
	s.pendingOutput = ""
	s.mu.Unlock()
	snapshot = s.manager.redactText(snapshot)
	pending = s.manager.redactText(pending)
	if err := persistConsoleTranscript(context.Background(), s.manager.db, s.id, snapshot, pending, now); err != nil {
		if pending != "" {
			s.mu.Lock()
			s.pendingOutput = pending + s.pendingOutput
			s.mu.Unlock()
		}
		logConsolePersistError("flush_transcript", s.id, err)
	}
}

func persistConsoleTranscript(ctx context.Context, database *sql.DB, sessionID int64, snapshot string, pending string, now string) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transcript persistence: %w", err)
	}
	defer tx.Rollback()

	if pending != "" {
		nextSeq, err := nextConsoleChunkSeqTx(tx, sessionID)
		if err != nil {
			return err
		}
		for len(pending) > 0 {
			chunk := pending
			if len(chunk) > maxConsoleChunkLength {
				chunk = pending[:maxConsoleChunkLength]
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO console_session_chunks (session_id, seq, data, created_at) VALUES (?, ?, ?, ?)`,
				sessionID,
				nextSeq,
				chunk,
				now,
			); err != nil {
				return fmt.Errorf("insert console transcript chunk: %w", err)
			}
			pending = pending[len(chunk):]
			nextSeq++
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE console_sessions SET transcript = ?, updated_at = ? WHERE id = ?`, snapshot, now, sessionID); err != nil {
		return fmt.Errorf("update console transcript snapshot: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transcript persistence: %w", err)
	}
	return nil
}

func nextConsoleChunkSeqTx(tx *sql.Tx, sessionID int64) (int64, error) {
	var nextSeq sql.NullInt64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM console_session_chunks WHERE session_id = ?`, sessionID).Scan(&nextSeq); err != nil {
		return 0, fmt.Errorf("read next console transcript chunk seq: %w", err)
	}
	if !nextSeq.Valid || nextSeq.Int64 < 1 {
		return 1, nil
	}
	return nextSeq.Int64, nil
}

func logConsolePersistError(operation string, sessionID int64, err error) {
	log.Printf("console session persistence failed operation=%s session_id=%d error=%v", operation, sessionID, err)
}

func (s *managedConsoleSession) broadcast(message ptyServerMessage) {
	s.mu.Lock()
	clients := make(map[*websocket.Conn]*sync.Mutex, len(s.clients))
	for ws, writeMu := range s.clients {
		clients[ws] = writeMu
	}
	s.mu.Unlock()
	for ws, writeMu := range clients {
		if err := writePTYMessage(ws, writeMu, message); err != nil {
			s.removeClient(ws)
			_ = ws.Close()
		}
	}
}
