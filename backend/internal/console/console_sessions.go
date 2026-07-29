package console

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
	"github.com/gorilla/websocket"
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
var ErrSessionChanged = errors.New("console session changed")
var ErrClientLimit = errors.New("console session client limit reached")
var ErrInputTooLarge = errors.New("console input is too large")
var ErrUnauthorized = errors.New("execution principal is not authorized for this console session")

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
	ID                     int64   `json:"id"`
	RuntimeID              int64   `json:"runtime_id"`
	Generation             int64   `json:"generation"`
	TargetName             string  `json:"target_name"`
	Name                   string  `json:"name"`
	Status                 string  `json:"status"`
	Transcript             string  `json:"transcript"`
	Error                  string  `json:"error"`
	Cols                   int     `json:"cols"`
	Rows                   int     `json:"rows"`
	CreatedAt              string  `json:"created_at"`
	UpdatedAt              string  `json:"updated_at"`
	ClosedAt               *string `json:"closed_at"`
	EnvironmentContentHash string  `json:"environment_content_hash,omitempty"`
}

type CreateRequest struct {
	RuntimeID              int64                        `json:"runtime_id"`
	Name                   string                       `json:"name"`
	CloseExisting          bool                         `json:"close_existing"`
	Cols                   int                          `json:"cols"`
	Rows                   int                          `json:"rows"`
	WaitForStart           bool                         `json:"wait_for_start"`
	Params                 map[string]any               `json:"params,omitempty"`
	Principal              executionprincipal.Principal `json:"-"`
	Environment            *sessionenv.Envelope         `json:"-"`
	PrepareEnvironment     EnvironmentPreparer          `json:"-"`
	EnvironmentContentHash string                       `json:"-"`
	ApprovalContextHash    string                       `json:"-"`
}

type EnvironmentPreparation struct {
	Environment  *sessionenv.Envelope
	Release      func()
	PostValidate func(context.Context) error
	Finalize     func(context.Context, SessionHandle) error
}

type EnvironmentPreparer func(context.Context, string) (EnvironmentPreparation, error)

type InputRequest struct {
	Data string `json:"data"`
}

type ExecResult struct {
	SessionID  int64
	Generation int64
	Command    string
	Output     string
	ExitCode   int
	Running    bool
	DurationMS int64
}

type SessionHandle struct {
	ID         int64 `json:"id"`
	RuntimeID  int64 `json:"runtime_id"`
	Generation int64 `json:"generation"`
}

func (h SessionHandle) Valid() bool {
	return h.ID > 0 && h.RuntimeID > 0 && h.Generation > 0
}

type RuntimeOpenRequest struct {
	RuntimeID      int64
	Generation     int64
	Rows           int
	Cols           int
	Params         map[string]any
	HasEnvironment bool
}

type RuntimeOpener func(context.Context, RuntimeOpenRequest) (*RuntimeSession, error)

type RuntimeSession struct {
	Stdin                    io.WriteCloser
	Stdout                   io.Reader
	Stderr                   io.Reader
	Wait                     func() error
	Resize                   func(cols int, rows int) error
	Close                    func() error
	ApplyEnvironment         func(context.Context, *sessionenv.Envelope) error
	PeerIdentity             string
	StartupInputAfterConnect string
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
	db            *sql.DB
	openRuntime   RuntimeOpener
	redact        func(string) string
	authorize     SessionAuthorizer
	sessionClosed func(SessionHandle)

	mu       sync.Mutex
	sessions map[int64]*managedConsoleSession

	lifecycleMu sync.Mutex
	lifecycle   map[int64]*sync.Mutex
}

type SessionAuthorization struct {
	Handle                 SessionHandle
	EnvironmentContentHash string
	ApprovalContextHash    string
}

type SessionOperation string

const (
	OperationAttach    SessionOperation = "attach"
	OperationInput     SessionOperation = "input"
	OperationExecute   SessionOperation = "execute"
	OperationObserve   SessionOperation = "observe"
	OperationInterrupt SessionOperation = "interrupt"
	OperationClose     SessionOperation = "close"
)

// SessionAuthorizer must invoke run only while the authorization decision
// remains valid. This keeps permission mutations from crossing the boundary
// between a successful check and the protected console operation.
type SessionAuthorizer func(
	context.Context,
	executionprincipal.Principal,
	SessionAuthorization,
	SessionOperation,
	func() error,
) error

func (m *Manager) redactText(value string) string {
	if m == nil || m.redact == nil || value == "" {
		return value
	}
	return m.redact(value)
}

func cloneParams(params map[string]any) map[string]any {
	if len(params) == 0 {
		return nil
	}
	clone := make(map[string]any, len(params))
	for key, value := range params {
		clone[key] = value
	}
	return clone
}

type managedConsoleSession struct {
	id                     int64
	runtimeID              int64
	generation             int64
	name                   string
	cols                   int
	rows                   int
	params                 map[string]any
	principal              executionprincipal.Principal
	environment            *sessionenv.Envelope
	prepareEnvironment     EnvironmentPreparer
	environmentContentHash string
	approvalContextHash    string
	exactRedactor          *sessionenv.Redactor
	stdoutExactRedactor    *sessionenv.Redactor
	stderrExactRedactor    *sessionenv.Redactor
	exactRedactionClosed   bool
	manager                *Manager

	ctx       context.Context
	cancel    context.CancelFunc
	start     chan struct{}
	done      chan struct{}
	startOnce sync.Once

	mu            sync.Mutex
	execMu        sync.Mutex
	status        string
	transcript    string
	rawTranscript string
	pendingOutput string
	errText       string
	stdin         io.WriteCloser
	runtime       *RuntimeSession
	clients       map[*websocket.Conn]*sync.Mutex
	activeExec    *consoleSessionActiveExec
	manualInput   manualInputCapture
	manualActive  *consoleSessionManualCapture
	manualPause   *consoleSessionManualPause
	filterUntil   time.Time
	persistTimer  *time.Timer
	startErr      error
}
