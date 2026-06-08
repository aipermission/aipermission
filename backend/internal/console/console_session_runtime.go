package console

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/execution"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

func (s *managedConsoleSession) run() {
	defer s.manager.remove(s.id)

	server, privateKey, err := s.manager.getMaterial(s.ctx, s.serverID)
	if err != nil {
		s.fail(fmt.Sprintf("resolve ssh material: %v", err))
		return
	}

	signer, err := ssh.ParsePrivateKey([]byte(privateKey.PrivateKey))
	if err != nil {
		s.fail(fmt.Sprintf("parse private key: %v", err))
		return
	}

	hostKeyCallback, err := execution.HostKeyCallback(s.manager.knownHosts)
	if err != nil {
		s.fail(fmt.Sprintf("load known_hosts: %v", err))
		return
	}

	config := &ssh.ClientConfig{
		User:            server.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         12 * time.Second,
	}

	address := net.JoinHostPort(server.Host, fmt.Sprintf("%d", server.Port))
	sshClient, err := ssh.Dial("tcp", address, config)
	if err != nil {
		s.fail(fmt.Sprintf("ssh dial: %v", err))
		return
	}
	s.mu.Lock()
	s.sshClient = sshClient
	s.mu.Unlock()
	defer sshClient.Close()

	sshSession, err := sshClient.NewSession()
	if err != nil {
		s.fail(fmt.Sprintf("new ssh session: %v", err))
		return
	}
	s.mu.Lock()
	s.sshSession = sshSession
	s.mu.Unlock()
	defer sshSession.Close()

	stdin, err := sshSession.StdinPipe()
	if err != nil {
		s.fail(fmt.Sprintf("stdin pipe: %v", err))
		return
	}
	s.mu.Lock()
	s.stdin = stdin
	s.mu.Unlock()

	stdout, err := sshSession.StdoutPipe()
	if err != nil {
		s.fail(fmt.Sprintf("stdout pipe: %v", err))
		return
	}
	stderr, err := sshSession.StderrPipe()
	if err != nil {
		s.fail(fmt.Sprintf("stderr pipe: %v", err))
		return
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sshSession.RequestPty("xterm-256color", s.rows, s.cols, modes); err != nil {
		s.fail(fmt.Sprintf("request pty: %v", err))
		return
	}
	if server.ForceShellCommand != "" {
		if err := sshSession.Start(server.ForceShellCommand); err != nil {
			s.fail(fmt.Sprintf("start forced shell command: %v", err))
			return
		}
	} else if err := sshSession.Shell(); err != nil {
		s.fail(fmt.Sprintf("start shell: %v", err))
		return
	}

	s.setStatus("connected", "")
	s.broadcast(ptyServerMessage{Type: "ready", Status: "connected", SessionID: s.id})

	if server.StartupInputAfterConnect != "" {
		if _, err := io.WriteString(stdin, server.StartupInputAfterConnect); err != nil {
			s.fail(fmt.Sprintf("write startup input: %v", err))
			return
		}
	}

	go s.pipe(stdout)
	go s.pipe(stderr)

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- sshSession.Wait()
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
	sshSession := s.sshSession
	s.mu.Unlock()
	if sshSession != nil {
		_ = sshSession.WindowChange(rows, cols)
	}
	if _, err := s.manager.db.Exec(`UPDATE console_sessions SET cols = ?, rows = ?, updated_at = ? WHERE id = ?`, cols, rows, time.Now().UTC().Format(time.RFC3339), s.id); err != nil {
		logConsolePersistError("resize", s.id, err)
	}
}

func (s *managedConsoleSession) close() {
	s.cancel()
	s.mu.Lock()
	sshSession := s.sshSession
	sshClient := s.sshClient
	s.mu.Unlock()
	if sshSession != nil {
		_ = sshSession.Close()
	}
	if sshClient != nil {
		_ = sshClient.Close()
	}
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
