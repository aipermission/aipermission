package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

const (
	maintenanceConsoleMaxInputBytes      = 64 << 10
	maintenanceConsoleMaxTranscriptBytes = 200 << 10
	maintenanceConsoleDefaultCols        = 120
	maintenanceConsoleDefaultRows        = 32
	maintenanceConsolePingInterval       = 25 * time.Second
)

type maintenanceConsoleClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

type maintenanceConsoleServerMessage struct {
	Type   string `json:"type"`
	Data   string `json:"data,omitempty"`
	Status string `json:"status,omitempty"`
	Shell  string `json:"shell,omitempty"`
}

type maintenanceConsoleRuntime struct {
	mu      sync.Mutex
	session *maintenanceConsoleSession
}

type maintenanceConsoleSession struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	pty        *os.File
	shell      string
	status     string
	transcript string
	cols       int
	rows       int
	clients    map[*websocket.Conn]*sync.Mutex
	closed     chan struct{}
	closeOnce  sync.Once
}

func newMaintenanceConsoleRuntime() *maintenanceConsoleRuntime {
	return &maintenanceConsoleRuntime{}
}

func (h maintenanceConsoleHandlers) status(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.activeRuntimeOrLocked(w); !ok {
		return
	}
	sessionStatus := "closed"
	shell := maintenanceConsoleShell()
	if h.maintenanceConsole != nil {
		if snapshot, ok := h.maintenanceConsole.snapshot(); ok {
			sessionStatus = snapshot.Status
			shell = snapshot.Shell
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":              true,
		"scope":                "local-ui-only",
		"mode":                 "realtime-pty",
		"shell":                shell,
		"status":               sessionStatus,
		"max_input_bytes":      maintenanceConsoleMaxInputBytes,
		"max_transcript_bytes": maintenanceConsoleMaxTranscriptBytes,
	})
}

func (h maintenanceConsoleHandlers) open(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	session, err := h.maintenanceConsole.open()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "maintenance console failed to start")
		return
	}
	snapshot := session.snapshot()
	h.writeAudit(r.Context(), runtime, "user", nil, 0, "maintenance_console.opened", map[string]any{
		"scope": "local-ui-only",
		"mode":  "realtime-pty",
		"shell": snapshot.Shell,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"opened":    true,
		"mode":      "realtime-pty",
		"shell":     snapshot.Shell,
		"status":    snapshot.Status,
		"opened_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h maintenanceConsoleHandlers) close(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	closed := false
	if h.maintenanceConsole != nil {
		closed = h.maintenanceConsole.close()
	}
	h.writeAudit(r.Context(), runtime, "user", nil, 0, "maintenance_console.closed", map[string]any{
		"scope":  "local-ui-only",
		"mode":   "realtime-pty",
		"closed": closed,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"closed":    true,
		"closed_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h maintenanceConsoleHandlers) attach(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.activeRuntimeOrLocked(w); !ok {
		return
	}
	session := h.maintenanceConsole.active()
	if session == nil {
		writeError(w, http.StatusConflict, "maintenance console is not open")
		return
	}
	ws, err := h.upgradeWebSocket(w, r)
	if err != nil {
		return
	}
	session.attach(ws)
}

type maintenanceConsoleSnapshot struct {
	Status     string
	Shell      string
	Transcript string
}

func (m *maintenanceConsoleRuntime) snapshot() (maintenanceConsoleSnapshot, bool) {
	if m == nil {
		return maintenanceConsoleSnapshot{}, false
	}
	m.mu.Lock()
	session := m.session
	m.mu.Unlock()
	if session == nil {
		return maintenanceConsoleSnapshot{}, false
	}
	return session.snapshot(), true
}

func (m *maintenanceConsoleRuntime) active() *maintenanceConsoleSession {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil || !m.session.isLive() {
		return nil
	}
	return m.session
}

func (m *maintenanceConsoleRuntime) open() (*maintenanceConsoleSession, error) {
	if m == nil {
		return nil, fmt.Errorf("maintenance console runtime is not initialized")
	}
	m.mu.Lock()
	if m.session != nil && m.session.isLive() {
		session := m.session
		m.mu.Unlock()
		return session, nil
	}
	if m.session != nil {
		m.session.close()
		m.session = nil
	}
	session, err := startMaintenanceConsoleSession()
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	m.session = session
	m.mu.Unlock()
	return session, nil
}

func (m *maintenanceConsoleRuntime) close() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	session := m.session
	m.session = nil
	m.mu.Unlock()
	if session == nil {
		return false
	}
	session.close()
	return true
}

func startMaintenanceConsoleSession() (*maintenanceConsoleSession, error) {
	shell := maintenanceConsoleShell()
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "AIPERMISSION_MAINTENANCE_CONSOLE=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setctty: true, Setsid: true}
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(maintenanceConsoleDefaultRows),
		Cols: uint16(maintenanceConsoleDefaultCols),
	})
	if err != nil {
		return nil, err
	}
	session := &maintenanceConsoleSession{
		cmd:     cmd,
		pty:     tty,
		shell:   shell,
		status:  "connected",
		cols:    maintenanceConsoleDefaultCols,
		rows:    maintenanceConsoleDefaultRows,
		clients: map[*websocket.Conn]*sync.Mutex{},
		closed:  make(chan struct{}),
	}
	go session.readLoop()
	go session.waitLoop()
	return session, nil
}

func maintenanceConsoleShell() string {
	for _, candidate := range []string{"/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "/bin/sh"
}

func (s *maintenanceConsoleSession) snapshot() maintenanceConsoleSnapshot {
	if s == nil {
		return maintenanceConsoleSnapshot{Status: "closed", Shell: maintenanceConsoleShell()}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return maintenanceConsoleSnapshot{
		Status:     s.status,
		Shell:      s.shell,
		Transcript: s.transcript,
	}
}

func (s *maintenanceConsoleSession) isLive() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status == "connected"
}

func (s *maintenanceConsoleSession) attach(ws *websocket.Conn) {
	writeMu := &sync.Mutex{}
	s.addClient(ws, writeMu)
	defer s.removeClient(ws)

	ws.SetReadLimit(maintenanceConsoleMaxInputBytes + 1024)
	_ = ws.SetReadDeadline(time.Now().Add(2 * maintenanceConsolePingInterval))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(2 * maintenanceConsolePingInterval))
	})
	stopPing := make(chan struct{})
	go maintenanceConsoleKeepAlive(ws, writeMu, stopPing)
	defer close(stopPing)

	for {
		var message maintenanceConsoleClientMessage
		if err := ws.ReadJSON(&message); err != nil {
			return
		}
		switch message.Type {
		case "input":
			if len(message.Data) > maintenanceConsoleMaxInputBytes {
				_ = writeMaintenanceConsoleMessage(ws, writeMu, maintenanceConsoleServerMessage{
					Type:   "error",
					Status: "error",
					Data:   "maintenance console input is too large",
				})
				continue
			}
			if err := s.writeInput(message.Data); err != nil {
				_ = writeMaintenanceConsoleMessage(ws, writeMu, maintenanceConsoleServerMessage{
					Type:   "error",
					Status: "error",
					Data:   err.Error(),
				})
			}
		case "resize":
			s.resize(message.Cols, message.Rows)
		}
	}
}

func (s *maintenanceConsoleSession) addClient(ws *websocket.Conn, writeMu *sync.Mutex) {
	s.mu.Lock()
	s.clients[ws] = writeMu
	snapshot := s.transcript
	status := s.status
	shell := s.shell
	s.mu.Unlock()
	_ = writeMaintenanceConsoleMessage(ws, writeMu, maintenanceConsoleServerMessage{
		Type:   "snapshot",
		Status: status,
		Shell:  shell,
		Data:   snapshot,
	})
	_ = writeMaintenanceConsoleMessage(ws, writeMu, maintenanceConsoleServerMessage{
		Type:   "ready",
		Status: status,
		Shell:  shell,
	})
}

func (s *maintenanceConsoleSession) removeClient(ws *websocket.Conn) {
	s.mu.Lock()
	delete(s.clients, ws)
	s.mu.Unlock()
	_ = ws.Close()
}

func (s *maintenanceConsoleSession) writeInput(data string) error {
	if data == "" {
		return nil
	}
	s.mu.Lock()
	tty := s.pty
	status := s.status
	s.mu.Unlock()
	if tty == nil || status != "connected" {
		return errors.New("maintenance console is not connected")
	}
	_, err := tty.Write([]byte(data))
	return err
}

func (s *maintenanceConsoleSession) resize(cols int, rows int) {
	if cols < 1 || rows < 1 {
		return
	}
	s.mu.Lock()
	tty := s.pty
	s.cols = cols
	s.rows = rows
	s.mu.Unlock()
	if tty != nil {
		_ = pty.Setsize(tty, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	}
}

func (s *maintenanceConsoleSession) readLoop() {
	buffer := make([]byte, 4096)
	for {
		n, err := s.pty.Read(buffer)
		if n > 0 {
			data := string(buffer[:n])
			s.appendTranscript(data)
			s.broadcast(maintenanceConsoleServerMessage{
				Type:   "output",
				Status: "connected",
				Shell:  s.shell,
				Data:   data,
			})
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !strings.Contains(strings.ToLower(err.Error()), "input/output error") {
				s.markClosed("error", err.Error())
			} else {
				s.markClosed("closed", "maintenance console closed")
			}
			return
		}
	}
}

func (s *maintenanceConsoleSession) waitLoop() {
	if s.cmd == nil {
		return
	}
	err := s.cmd.Wait()
	if err != nil {
		s.markClosed("closed", "maintenance console process exited")
		return
	}
	s.markClosed("closed", "maintenance console process exited")
}

func (s *maintenanceConsoleSession) appendTranscript(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transcript = tailStringByBytes(s.transcript+data, maintenanceConsoleMaxTranscriptBytes)
}

func (s *maintenanceConsoleSession) markClosed(status string, data string) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.status = status
		if s.pty != nil {
			_ = s.pty.Close()
		}
		s.mu.Unlock()
		close(s.closed)
		s.broadcast(maintenanceConsoleServerMessage{Type: "exit", Status: status, Shell: s.shell, Data: data})
	})
}

func (s *maintenanceConsoleSession) close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.status = "closed"
		tty := s.pty
		cmd := s.cmd
		s.mu.Unlock()
		if tty != nil {
			_ = tty.Close()
		}
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGHUP)
			time.AfterFunc(250*time.Millisecond, func() {
				_ = cmd.Process.Kill()
			})
		}
		close(s.closed)
		s.broadcast(maintenanceConsoleServerMessage{
			Type:   "exit",
			Status: "closed",
			Shell:  s.shell,
			Data:   "maintenance console closed",
		})
	})
}

func (s *maintenanceConsoleSession) broadcast(message maintenanceConsoleServerMessage) {
	s.mu.Lock()
	clients := make(map[*websocket.Conn]*sync.Mutex, len(s.clients))
	for ws, writeMu := range s.clients {
		clients[ws] = writeMu
	}
	s.mu.Unlock()
	for ws, writeMu := range clients {
		if err := writeMaintenanceConsoleMessage(ws, writeMu, message); err != nil {
			s.removeClient(ws)
		}
	}
}

func writeMaintenanceConsoleMessage(ws *websocket.Conn, writeMu *sync.Mutex, message maintenanceConsoleServerMessage) error {
	if writeMu != nil {
		writeMu.Lock()
		defer writeMu.Unlock()
	}
	return ws.WriteJSON(message)
}

func maintenanceConsoleKeepAlive(ws *websocket.Conn, writeMu *sync.Mutex, stop <-chan struct{}) {
	ticker := time.NewTicker(maintenanceConsolePingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if writeMu != nil {
				writeMu.Lock()
			}
			err := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second))
			if writeMu != nil {
				writeMu.Unlock()
			}
			if err != nil {
				return
			}
		case <-stop:
			return
		}
	}
}

func tailStringByBytes(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	return value[len(value)-maxBytes:]
}
