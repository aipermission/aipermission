package console

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sshkeys"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

const (
	maxConsoleTranscriptLength  = 200000
	maxConsoleSnapshotLength    = 50000
	maxConsoleChunkLength       = 32768
	maxConsolePendingFlushSize  = maxConsoleChunkLength * 4
	maxActiveConsoleSessions    = 32
	maxConsoleClientsPerSession = 8
	maxConsoleInputBytes        = 65536
	maxPTYClientMessageBytes    = maxConsoleInputBytes + 4096
	ptyPongWait                 = 75 * time.Second
	ptyPingInterval             = 25 * time.Second
	ptyInputMinInterval         = 20 * time.Millisecond
	ptyResizeMinInterval        = 100 * time.Millisecond
)

var ErrCommandActive = errors.New("another command is already running on this console session")
var ErrNotFound = errors.New("console session not found")
var ErrSessionLimit = errors.New("active console session limit reached")
var ErrClientLimit = errors.New("console session client limit reached")
var ErrInputTooLarge = errors.New("console input is too large")

type InactiveError struct {
	Status string
	Detail string
}

func (e InactiveError) Error() string {
	if e.Detail == "" {
		return "console session is " + e.Status
	}
	return "console session is " + e.Status + ": " + e.Detail
}

type Record struct {
	ID         int64   `json:"id"`
	ServerID   int64   `json:"server_id"`
	ServerName string  `json:"server_name"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	Transcript string  `json:"transcript"`
	Error      string  `json:"error"`
	Cols       int     `json:"cols"`
	Rows       int     `json:"rows"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	ClosedAt   *string `json:"closed_at"`
}

type Target struct {
	ID                       int64
	Name                     string
	Host                     string
	Port                     int
	Username                 string
	StartupInputAfterConnect string
	ForceShellCommand        string
}

type CreateRequest struct {
	ServerID      int64  `json:"server_id"`
	Name          string `json:"name"`
	CloseExisting bool   `json:"close_existing"`
	Cols          int    `json:"cols"`
	Rows          int    `json:"rows"`
}

type InputRequest struct {
	Data string `json:"data"`
}

type ExecResult struct {
	SessionID  int64
	Command    string
	Output     string
	ExitCode   int
	Running    bool
	DurationMS int64
}

type consoleSessionActiveExec struct {
	Command     string
	Marker      string
	StartOffset int
	Started     time.Time
}

type consoleSessionManualCapture struct {
	RequestID                int64
	Command                  string
	StartOffset              int
	ResumePrompt             string
	Started                  time.Time
	CompletionTrackingReason string
}

type consoleSessionManualPause struct {
	Prompt      string
	Reason      string
	StartOffset int
}

type Manager struct {
	db          *sql.DB
	getMaterial func(context.Context, int64) (Target, sshkeys.PrivateKey, error)
	knownHosts  string
	redact      func(string) string

	mu       sync.Mutex
	sessions map[int64]*managedConsoleSession
}

func (m *Manager) redactText(value string) string {
	if m == nil || m.redact == nil || value == "" {
		return value
	}
	return m.redact(value)
}

type managedConsoleSession struct {
	id       int64
	serverID int64
	name     string
	cols     int
	rows     int
	manager  *Manager

	ctx    context.Context
	cancel context.CancelFunc

	mu            sync.Mutex
	execMu        sync.Mutex
	status        string
	transcript    string
	rawTranscript string
	pendingOutput string
	errText       string
	stdin         io.WriteCloser
	sshClient     *ssh.Client
	sshSession    *ssh.Session
	clients       map[*websocket.Conn]*sync.Mutex
	activeExec    *consoleSessionActiveExec
	manualInput   manualInputCapture
	manualActive  *consoleSessionManualCapture
	manualPause   *consoleSessionManualPause
	filterUntil   time.Time
	persistTimer  *time.Timer
}
