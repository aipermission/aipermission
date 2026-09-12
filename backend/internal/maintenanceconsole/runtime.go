package maintenanceconsole

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

const (
	MaxInputBytes                        = 64 << 10
	MaxTranscriptBytes                   = 200 << 10
	maintenanceConsoleDefaultCols        = 120
	maintenanceConsoleDefaultRows        = 32
	maintenanceConsolePingInterval       = 25 * time.Second
	maintenanceConsoleWriteTimeout       = 2 * time.Second
	maintenanceConsoleProcessGracePeriod = 750 * time.Millisecond
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

type Runtime struct {
	mu      sync.Mutex
	session *Session
}

type Session struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	pty         *os.File
	shell       string
	status      string
	transcript  string
	cols        int
	rows        int
	clients     map[*websocket.Conn]*sync.Mutex
	closed      chan struct{}
	processDone chan struct{}
	stateOnce   sync.Once
	stopOnce    sync.Once
}

func NewRuntime() *Runtime {
	return &Runtime{}
}

var _ gatewayoperations.MaintenanceConsoleRuntime = (*Runtime)(nil)

func (m *Runtime) Descriptor() gatewayoperations.MaintenanceConsoleDescriptor {
	return gatewayoperations.MaintenanceConsoleDescriptor{
		Supported:          Supported(),
		Shell:              Shell(),
		MaxInputBytes:      MaxInputBytes,
		MaxTranscriptBytes: MaxTranscriptBytes,
	}
}

func (m *Runtime) Snapshot() (gatewayoperations.MaintenanceConsoleSnapshot, bool) {
	if m == nil {
		return gatewayoperations.MaintenanceConsoleSnapshot{}, false
	}
	m.mu.Lock()
	session := m.session
	m.mu.Unlock()
	if session == nil {
		return gatewayoperations.MaintenanceConsoleSnapshot{}, false
	}
	return session.Snapshot(), true
}

func (m *Runtime) activeSession() *Session {
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

func (m *Runtime) Open() (gatewayoperations.MaintenanceConsoleSnapshot, error) {
	if m == nil {
		return gatewayoperations.MaintenanceConsoleSnapshot{}, fmt.Errorf("maintenance console runtime is not initialized")
	}
	m.mu.Lock()
	if m.session != nil && m.session.isLive() {
		session := m.session
		m.mu.Unlock()
		return session.Snapshot(), nil
	}
	if m.session != nil {
		m.session.Close()
		m.session = nil
	}
	session, err := startMaintenanceConsoleSession()
	if err != nil {
		m.mu.Unlock()
		return gatewayoperations.MaintenanceConsoleSnapshot{}, err
	}
	m.session = session
	m.mu.Unlock()
	return session.Snapshot(), nil
}

func (m *Runtime) Active() bool {
	return m.activeSession() != nil
}

func (m *Runtime) Attach(ws *websocket.Conn) {
	session := m.activeSession()
	if session == nil {
		_ = ws.Close()
		return
	}
	session.Attach(ws)
}

func (m *Runtime) Close() bool {
	session := m.detach()
	if session == nil {
		return false
	}
	session.Close()
	return true
}

func (m *Runtime) detach() *Session {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	session := m.session
	m.session = nil
	m.mu.Unlock()
	return session
}

func startMaintenanceConsoleSession() (*Session, error) {
	shell := Shell()
	cmd, err := maintenanceConsoleCommand(shell, os.Environ())
	if err != nil {
		return nil, err
	}
	if err := configureMaintenanceConsoleProcess(cmd); err != nil {
		return nil, err
	}
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(maintenanceConsoleDefaultRows),
		Cols: uint16(maintenanceConsoleDefaultCols),
	})
	if err != nil {
		return nil, err
	}
	session := &Session{
		cmd:         cmd,
		pty:         tty,
		shell:       shell,
		status:      "connected",
		cols:        maintenanceConsoleDefaultCols,
		rows:        maintenanceConsoleDefaultRows,
		clients:     map[*websocket.Conn]*sync.Mutex{},
		closed:      make(chan struct{}),
		processDone: make(chan struct{}),
	}
	go session.readLoop()
	go session.waitLoop()
	return session, nil
}

func maintenanceConsoleCommand(shell string, sourceEnvironment []string) (*exec.Cmd, error) {
	arguments := []string{}
	if shell == "/bin/bash" {
		arguments = append(arguments, "--noprofile", "--norc")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve maintenance console supervisor: %w", err)
	}
	cmd := exec.Command(executable, append([]string{maintenanceConsoleSupervisorArgument, shell}, arguments...)...)
	cmd.Env = maintenanceConsoleEnvironment(sourceEnvironment)
	return cmd, nil
}

func maintenanceConsoleEnvironment(source []string) []string {
	allowed := map[string]struct{}{
		"HOME":              {},
		"LANG":              {},
		"LANGUAGE":          {},
		"LC_ADDRESS":        {},
		"LC_ALL":            {},
		"LC_COLLATE":        {},
		"LC_CTYPE":          {},
		"LC_IDENTIFICATION": {},
		"LC_MEASUREMENT":    {},
		"LC_MESSAGES":       {},
		"LC_MONETARY":       {},
		"LC_NAME":           {},
		"LC_NUMERIC":        {},
		"LC_PAPER":          {},
		"LC_TELEPHONE":      {},
		"LC_TIME":           {},
		"LOGNAME":           {},
		"PATH":              {},
		"SHELL":             {},
		"TZ":                {},
		"USER":              {},
	}
	environment := make([]string, 0, len(allowed)+4)
	for _, entry := range source {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		if _, ok := allowed[key]; !ok {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment,
		"TERM=xterm-256color",
		"HISTFILE=/dev/null",
		"HISTSIZE=0",
		"HISTFILESIZE=0",
		"AIPERMISSION_MAINTENANCE_CONSOLE=1",
	)
}

func Shell() string {
	for _, candidate := range []string{"/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "/bin/sh"
}

func (s *Session) Snapshot() gatewayoperations.MaintenanceConsoleSnapshot {
	if s == nil {
		return gatewayoperations.MaintenanceConsoleSnapshot{Status: "closed", Shell: Shell()}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return gatewayoperations.MaintenanceConsoleSnapshot{
		Status:     s.status,
		Shell:      s.shell,
		Transcript: s.transcript,
	}
}

func (s *Session) Done() <-chan struct{} {
	if s == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return s.closed
}

func (s *Session) ProcessDone() <-chan struct{} {
	if s == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return s.processDone
}

func (s *Session) isLive() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status == "connected"
}

func (s *Session) Attach(ws *websocket.Conn) {
	writeMu := &sync.Mutex{}
	if !s.addClient(ws, writeMu) {
		_ = writeMaintenanceConsoleMessage(ws, writeMu, maintenanceConsoleServerMessage{
			Type:   "error",
			Status: "closed",
			Data:   "maintenance console is no longer open",
		})
		_ = ws.Close()
		return
	}
	defer s.removeClient(ws)

	ws.SetReadLimit(MaxInputBytes + 1024)
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
			if len(message.Data) > MaxInputBytes {
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

func (s *Session) addClient(ws *websocket.Conn, writeMu *sync.Mutex) bool {
	snapshot, ok := s.registerClient(ws, writeMu)
	if !ok {
		return false
	}
	_ = writeMaintenanceConsoleMessage(ws, writeMu, maintenanceConsoleServerMessage{
		Type:   "snapshot",
		Status: snapshot.Status,
		Shell:  snapshot.Shell,
		Data:   snapshot.Transcript,
	})
	_ = writeMaintenanceConsoleMessage(ws, writeMu, maintenanceConsoleServerMessage{
		Type:   "ready",
		Status: snapshot.Status,
		Shell:  snapshot.Shell,
	})
	return true
}

func (s *Session) registerClient(ws *websocket.Conn, writeMu *sync.Mutex) (gatewayoperations.MaintenanceConsoleSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != "connected" || s.pty == nil {
		return gatewayoperations.MaintenanceConsoleSnapshot{}, false
	}
	s.clients[ws] = writeMu
	return gatewayoperations.MaintenanceConsoleSnapshot{Status: s.status, Shell: s.shell, Transcript: s.transcript}, true
}

func (s *Session) removeClient(ws *websocket.Conn) {
	s.mu.Lock()
	delete(s.clients, ws)
	s.mu.Unlock()
	_ = ws.Close()
}

func (s *Session) writeInput(data string) error {
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

func (s *Session) resize(cols int, rows int) {
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

func (s *Session) readLoop() {
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
				s.terminate("error", err.Error())
			} else {
				s.terminate("closed", "maintenance console closed")
			}
			return
		}
	}
}

func (s *Session) waitLoop() {
	if s.cmd == nil {
		s.markProcessDone()
		return
	}
	err := s.cmd.Wait()
	s.markProcessDone()
	if err != nil {
		s.markClosed("closed", "maintenance console process exited")
		return
	}
	s.markClosed("closed", "maintenance console process exited")
}

func (s *Session) appendTranscript(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transcript = tailStringByBytes(s.transcript+data, MaxTranscriptBytes)
}

func (s *Session) markClosed(status string, data string) {
	s.stateOnce.Do(func() {
		s.mu.Lock()
		s.status = status
		if s.pty != nil {
			_ = s.pty.Close()
		}
		s.mu.Unlock()
		close(s.closed)
		s.closeClients()
	})
}

func (s *Session) Close() {
	s.terminate("closed", "maintenance console closed")
}

func (s *Session) terminate(status string, data string) {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		tty := s.pty
		cmd := s.cmd
		if s.status == "connected" {
			s.status = "closing"
		}
		s.mu.Unlock()
		if tty != nil {
			_ = tty.Close()
		}
		if cmd != nil && cmd.Process != nil {
			terminateMaintenanceConsoleSupervisor(cmd.Process.Pid)
			waitForMaintenanceConsoleProcess(s.processDone, maintenanceConsoleProcessGracePeriod)
		}
	})
	s.markClosed(status, data)
}

func (s *Session) markProcessDone() {
	select {
	case <-s.processDone:
		return
	default:
		close(s.processDone)
	}
}

func waitForMaintenanceConsoleSupervisorExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for maintenanceConsoleProcessExists(pid) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}

func waitForMaintenanceConsoleProcess(done <-chan struct{}, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

func (s *Session) broadcast(message maintenanceConsoleServerMessage) {
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
	if err := ws.SetWriteDeadline(time.Now().Add(maintenanceConsoleWriteTimeout)); err != nil {
		return err
	}
	return ws.WriteJSON(message)
}

func (s *Session) closeClients() {
	s.mu.Lock()
	clients := s.clients
	s.clients = map[*websocket.Conn]*sync.Mutex{}
	s.mu.Unlock()
	for ws := range clients {
		_ = ws.Close()
	}
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
