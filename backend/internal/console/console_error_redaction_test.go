package console

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
	"github.com/gorilla/websocket"
)

type consoleFailingWriter struct{ cause error }

func (writer consoleFailingWriter) Write([]byte) (int, error) { return 0, writer.cause }
func (consoleFailingWriter) Close() error                     { return nil }

func TestConsoleExactRedactionCoversLifecycleErrors(t *testing.T) {
	for _, phase := range []string{"open", "apply", "post-validate", "finalize", "done", "startup-input"} {
		for _, mode := range []string{"off", "basic"} {
			t.Run(phase+"/"+mode, func(t *testing.T) { testConsoleLifecycleError(t, phase, mode) })
		}
	}
}

func TestConsoleErrorProjectionKeepsClassificationAndSafeLifetime(t *testing.T) {
	_, manager, session := newManualHistoryTestSession(t)
	manager.redact = securitypolicy.RedactBasic
	session.start = make(chan struct{})
	plain := errors.New("ordinary startup failure")
	session.markStarted(plain)
	if got := session.waitStart(t.Context()); got != plain {
		t.Fatalf("ordinary classification changed: %v", got)
	}
	if got := session.redactForPersistence("password=ordinary-secret"); got != "password=[REDACTED]" {
		t.Fatalf("optional policy without environment: %q", got)
	}

	environment, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{Name: "MARKER_TEST", Value: []byte("REDACTED VAULT")}})
	if err != nil {
		t.Fatal(err)
	}
	defer environment.Destroy()
	session.environment = environment
	session.fail("runtime failure REDACTED VAULT")
	if session.errText != "runtime failure [REDACTED VAULT VALUE]" || session.finalMessage != session.errText {
		t.Fatalf("repeated error projection changed placeholder: %q / %q", session.errText, session.finalMessage)
	}
	session.closeExactRedactor()
	if got := session.redactForPersistence("late runtime failure REDACTED VAULT"); got != "[REDACTED VAULT VALUE]" {
		t.Fatalf("closed redactor leaked text: %q", got)
	}
}

func testConsoleLifecycleError(t *testing.T, phase, mode string) {
	t.Helper()
	database, manager, session := newManualHistoryTestSession(t)
	const canary = "harmless-console-vault-value-f1832"
	if securitypolicy.RedactBasic(canary) != canary {
		t.Fatal("canary must bypass basic patterns")
	}
	if mode == "basic" {
		manager.redact = securitypolicy.RedactBasic
	}
	environment, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{Name: "FIXTURE_VALUE", Value: []byte(canary)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(environment.Destroy)
	cause := errors.New("runtime failure " + canary)
	if phase == "open" {
		session.environment = environment
	}
	session.ctx, session.cancel = context.WithCancel(t.Context())
	t.Cleanup(session.cancel)
	session.start = make(chan struct{})
	session.done = make(chan struct{})
	session.prepareEnvironment = func(context.Context, string) (EnvironmentPreparation, error) {
		return EnvironmentPreparation{
			Environment: environment,
			PostValidate: func(context.Context) error {
				if phase == "post-validate" {
					return cause
				}
				return nil
			},
			Finalize: func(context.Context, SessionHandle) error {
				if phase == "finalize" {
					return cause
				}
				return nil
			},
		}, nil
	}
	manager.openRuntime = func(context.Context, RuntimeOpenRequest) (*RuntimeSession, error) {
		if phase == "open" {
			return nil, cause
		}
		done := make(chan error, 1)
		if phase == "done" {
			done <- cause
		} else {
			done <- nil
		}
		close(done)
		runtime := &RuntimeSession{
			Stdin: &recordingWriteCloser{}, Output: testRuntimeOutput(), Done: done,
			Close: func() error { return nil },
			ApplyEnvironment: func(context.Context, *sessionenv.Envelope) error {
				if phase == "apply" {
					return cause
				}
				return nil
			},
		}
		if phase == "startup-input" {
			runtime.Stdin = consoleFailingWriter{cause}
			runtime.StartupInputAfterConnect = "fixture\n"
		}
		return runtime, nil
	}
	client := consoleErrorClient(t, session)
	go session.run()
	var messages strings.Builder
	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for {
		var message ptyServerMessage
		if err := client.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		messages.WriteString(message.Data)
		if message.Type == "exit" {
			break
		}
	}
	select {
	case <-session.done:
	case <-time.After(5 * time.Second):
		t.Fatal("session did not finalize")
	}
	startErr := session.waitStart(t.Context())
	if phase == "open" || phase == "apply" || phase == "post-validate" || phase == "finalize" {
		if !errors.Is(startErr, cause) {
			t.Fatalf("startup error lost classification: %v", startErr)
		}
		if strings.Contains(startErr.Error(), canary) {
			t.Fatal("WaitForStart exposed Vault value")
		}
	} else if startErr != nil {
		t.Fatalf("connected startup failed: %v", startErr)
	}
	record, err := manager.Get(t.Context(), session.id)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := database.QueryRow(`SELECT error FROM console_sessions WHERE id = ?`, session.id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	for surface, value := range map[string]string{"websocket": messages.String(), "record": record.Error, "database": stored, "final message": session.finalMessage} {
		if strings.Contains(value, canary) || !strings.Contains(value, "[REDACTED VAULT VALUE]") {
			t.Fatalf("unsafe %s: %q", surface, value)
		}
	}
}

func consoleErrorClient(t *testing.T, session *managedConsoleSession) *websocket.Conn {
	t.Helper()
	attached := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		session.mu.Lock()
		session.clients[conn] = &sync.Mutex{}
		session.mu.Unlock()
		attached <- conn
	}))
	t.Cleanup(server.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	select {
	case conn := <-attached:
		t.Cleanup(func() { _ = conn.Close() })
	case <-time.After(time.Second):
		t.Fatal("WebSocket fixture did not attach")
	}
	return client
}
