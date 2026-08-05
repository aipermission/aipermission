package console

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
	"github.com/gorilla/websocket"
)

func TestManagedConsoleSessionCommandResultParsing(t *testing.T) {
	session := &managedConsoleSession{
		status:        "connected",
		rawTranscript: "prompt\ncommand output\n__AIPERMISSION_EXIT_1:42\nnext prompt",
	}

	output, exitCode, completed, err := session.checkCommandResult(0, "__AIPERMISSION_EXIT_1")
	if err != nil {
		t.Fatalf("check result: %v", err)
	}
	if !completed || exitCode != 42 || output != "prompt\ncommand output" {
		t.Fatalf("unexpected result completed=%v exit=%d output=%q", completed, exitCode, output)
	}
}

func TestManagedConsoleSessionReportsClosedBeforeCommandMarker(t *testing.T) {
	session := &managedConsoleSession{
		status:        "closed",
		rawTranscript: "partial output",
	}

	output, _, completed, err := session.checkCommandResult(0, "missing")
	if err == nil || completed || output != "partial output" {
		t.Fatalf("expected closed error with partial output, completed=%v output=%q err=%v", completed, output, err)
	}
}

func TestConsoleTranscriptLimitKeepsTail(t *testing.T) {
	value := strings.Repeat("a", maxConsoleTranscriptLength+20)
	limited := limitConsoleTranscript(value)
	if len(limited) != maxConsoleTranscriptLength {
		t.Fatalf("unexpected limited length: %d", len(limited))
	}
	if limited != value[20:] {
		t.Fatalf("transcript should keep the newest tail")
	}
}

func TestConsoleSessionManagerActiveForServerUsesNewestLiveSession(t *testing.T) {
	manager := &Manager{
		sessions: map[int64]*managedConsoleSession{
			1: {id: 1, runtimeID: 10, status: "connected"},
			2: {id: 2, runtimeID: 10, status: "closed"},
			3: {id: 3, runtimeID: 10, status: "connected"},
			4: {id: 4, runtimeID: 11, status: "connected"},
		},
	}

	selected := manager.activeForRuntime(10)
	if selected == nil || selected.id != 3 {
		t.Fatalf("expected newest live session 3, got %#v", selected)
	}
}

func TestConsoleSessionManagerRequiresExactGeneration(t *testing.T) {
	manager := &Manager{sessions: map[int64]*managedConsoleSession{
		7: {
			id: 7, runtimeID: 10, generation: 3, status: "connected",
			principal: testExecutionPrincipal(),
		},
	}}
	if _, err := manager.exactSession(SessionHandle{ID: 7, RuntimeID: 10, Generation: 2}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale generation should not resolve, got %v", err)
	}
	if session, err := manager.exactSession(SessionHandle{ID: 7, RuntimeID: 10, Generation: 3}); err != nil || session.id != 7 {
		t.Fatalf("exact generation should resolve, session=%#v err=%v", session, err)
	}
}

func TestConsoleSessionManagerDeniesTokenWithoutVaultLease(t *testing.T) {
	local := testExecutionPrincipal()
	token, err := executionprincipal.MCPToken(9, local.WorkspaceID, local.RuntimeInstanceID)
	if err != nil {
		t.Fatal(err)
	}
	session := &managedConsoleSession{
		id: 4, runtimeID: 5, generation: 6, status: "connected",
		principal: local, environmentContentHash: "vault-context",
	}
	manager := &Manager{sessions: map[int64]*managedConsoleSession{4: session}}
	if err := manager.authorizeOperation(context.Background(), token, session, OperationObserve, nil); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("token without an exact lease should be denied, got %v", err)
	}
	if err := manager.authorizeOperation(context.Background(), local, session, OperationObserve, nil); err != nil {
		t.Fatalf("local operator should retain access: %v", err)
	}
}

func TestConsoleSessionManagerRecoversOnlyOwnedStaleVaultSession(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	local := testExecutionPrincipal()
	owner, err := executionprincipal.MCPToken(9, local.WorkspaceID, local.RuntimeInstanceID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := executionprincipal.MCPToken(10, local.WorkspaceID, local.RuntimeInstanceID)
	if err != nil {
		t.Fatal(err)
	}
	session.principal = owner
	session.environmentContentHash = "vault-context"
	session.approvalContextHash = "approval-context"
	sessionCtx, cancel := context.WithCancel(context.Background())
	session.ctx = sessionCtx
	session.cancel = cancel
	manager.sessions[session.id] = session
	manager.authorize = func(
		_ context.Context,
		_ executionprincipal.Principal,
		_ SessionAuthorization,
		_ SessionOperation,
		_ func() error,
	) error {
		return ErrUnauthorized
	}

	if err := manager.CloseRuntime(context.Background(), owner, session.runtimeID); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("normal close should preserve the stale lease boundary, got %v", err)
	}
	callbackCalled := false
	if _, err := manager.RecoverRuntime(context.Background(), other, session.runtimeID, func() error {
		callbackCalled = true
		return nil
	}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("another token recovered the Vault session, got %v", err)
	}
	if callbackCalled {
		t.Fatal("recovery callback ran before authorization completed")
	}
	var status string
	if err := database.QueryRow(`SELECT status FROM console_sessions WHERE id = ?`, session.id).Scan(&status); err != nil {
		t.Fatalf("read session after denied recovery: %v", err)
	}
	if status != "connected" {
		t.Fatalf("denied recovery changed session status to %q", status)
	}

	prepareErr := errors.New("prepare recovery")
	if _, err := manager.RecoverRuntime(context.Background(), owner, session.runtimeID, func() error {
		return prepareErr
	}); !errors.Is(err, prepareErr) {
		t.Fatalf("recovery preparation error = %v", err)
	}
	if err := database.QueryRow(`SELECT status FROM console_sessions WHERE id = ?`, session.id).Scan(&status); err != nil {
		t.Fatalf("read session after failed recovery preparation: %v", err)
	}
	if status != "connected" {
		t.Fatalf("failed recovery preparation changed session status to %q", status)
	}

	closedIDs, err := manager.RecoverRuntime(context.Background(), owner, session.runtimeID, func() error {
		callbackCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("owning token could not recover stale Vault session: %v", err)
	}
	if len(closedIDs) != 1 || closedIDs[0] != session.id {
		t.Fatalf("recovered session ids = %v", closedIDs)
	}
	if err := database.QueryRow(`SELECT status FROM console_sessions WHERE id = ?`, session.id).Scan(&status); err != nil {
		t.Fatalf("read recovered session: %v", err)
	}
	if status != "closed" {
		t.Fatalf("recovered session status = %q", status)
	}
}

func TestConsoleSessionManagerSerializesAuthorizationWithInputWrite(t *testing.T) {
	local := testExecutionPrincipal()
	token, err := executionprincipal.MCPToken(9, local.WorkspaceID, local.RuntimeInstanceID)
	if err != nil {
		t.Fatal(err)
	}
	writeStarted := make(chan struct{})
	allowWrite := make(chan struct{})
	stdin := &blockingWriteCloser{started: writeStarted, proceed: allowWrite}
	session := &managedConsoleSession{
		id: 4, runtimeID: 5, generation: 6, status: "connected",
		principal: local, environmentContentHash: "vault-context",
		stdin: stdin, clients: map[*websocket.Conn]*sync.Mutex{},
	}
	manager := &Manager{sessions: map[int64]*managedConsoleSession{4: session}}
	var gate sync.Mutex
	allowed := true
	manager.authorize = func(
		_ context.Context,
		_ executionprincipal.Principal,
		_ SessionAuthorization,
		_ SessionOperation,
		run func() error,
	) error {
		gate.Lock()
		defer gate.Unlock()
		if !allowed {
			return ErrUnauthorized
		}
		return run()
	}

	inputDone := make(chan error, 1)
	go func() {
		inputDone <- manager.Input(context.Background(), token, session.id, "x")
	}()
	<-writeStarted

	revokeDone := make(chan struct{})
	go func() {
		gate.Lock()
		allowed = false
		gate.Unlock()
		close(revokeDone)
	}()
	select {
	case <-revokeDone:
		t.Fatal("authorization mutation crossed an in-flight input write")
	case <-time.After(25 * time.Millisecond):
	}

	close(allowWrite)
	if err := <-inputDone; err != nil {
		t.Fatalf("authorized input: %v", err)
	}
	<-revokeDone
	if err := manager.Input(context.Background(), token, session.id, "y"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("input after revocation should be denied, got %v", err)
	}
}

func TestManagedConsoleSessionRedactsVaultValueAcrossOutputChunks(t *testing.T) {
	envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
		Name: "MY_PROJECT_TOKEN", Value: []byte("secret-value"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer envelope.Destroy()
	redactor, err := envelope.ExactValueRedactor()
	if err != nil {
		t.Fatal(err)
	}
	session := &managedConsoleSession{
		id: 1, generation: 1, status: "connected",
		manager: &Manager{redact: func(value string) string { return value }},
		clients: map[*websocket.Conn]*sync.Mutex{}, exactRedactor: redactor,
	}
	session.appendOutput("before secret-")
	session.appendOutput("value after")
	session.closeExactRedactor()
	if strings.Contains(session.rawTranscript, "secret-value") || strings.Contains(session.transcript, "secret-value") {
		t.Fatalf("vault value leaked into console state: raw=%q display=%q", session.rawTranscript, session.transcript)
	}
	if !strings.Contains(session.rawTranscript, "[REDACTED VAULT VALUE]") {
		t.Fatalf("redaction marker missing: %q", session.rawTranscript)
	}
}

func TestManagedConsoleSessionKeepsStdoutAndStderrRedactionStateIndependent(t *testing.T) {
	envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
		Name: "MY_PROJECT_TOKEN", Value: []byte("secret-value"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer envelope.Destroy()
	persistenceRedactor, err := envelope.ExactValueRedactor()
	if err != nil {
		t.Fatal(err)
	}
	stdoutRedactor, err := envelope.ExactValueRedactor()
	if err != nil {
		t.Fatal(err)
	}
	stderrRedactor, err := envelope.ExactValueRedactor()
	if err != nil {
		t.Fatal(err)
	}
	session := &managedConsoleSession{
		id: 1, generation: 1, status: "connected",
		manager:       &Manager{redact: func(value string) string { return value }},
		clients:       map[*websocket.Conn]*sync.Mutex{},
		exactRedactor: persistenceRedactor, stdoutExactRedactor: stdoutRedactor, stderrExactRedactor: stderrRedactor,
	}

	session.appendStreamOutput("stdout secret-", stdoutRedactor)
	session.appendStreamOutput("stderr remains visible\n", stderrRedactor)
	session.appendStreamOutput("value complete\n", stdoutRedactor)
	session.closeExactRedactor()

	if strings.Contains(session.rawTranscript, "secret-value") || strings.Contains(session.transcript, "secret-value") {
		t.Fatalf("interleaved stream leaked Vault value: raw=%q display=%q", session.rawTranscript, session.transcript)
	}
	if !strings.Contains(session.rawTranscript, "[REDACTED VAULT VALUE]") || !strings.Contains(session.rawTranscript, "stderr remains visible") {
		t.Fatalf("independent streams were not preserved safely: %q", session.rawTranscript)
	}
}

func TestManagedConsoleSessionRedactsVaultValueFromDisplayAndPersistenceText(t *testing.T) {
	envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
		Name: "MY_PROJECT_TOKEN", Value: []byte("secret-value"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer envelope.Destroy()
	redactor, err := envelope.ExactValueRedactor()
	if err != nil {
		t.Fatal(err)
	}
	session := &managedConsoleSession{
		id: 1, generation: 1, status: "connected",
		manager: &Manager{redact: func(value string) string { return value }},
		clients: map[*websocket.Conn]*sync.Mutex{}, exactRedactor: redactor,
	}
	session.appendDisplayOutput("[AI command]\n$ printf secret-value\n")
	if strings.Contains(session.transcript, "secret-value") {
		t.Fatalf("display output leaked Vault value: %q", session.transcript)
	}
	if got := session.redactForPersistence("manual secret-value"); got != "manual [REDACTED VAULT VALUE]" {
		t.Fatalf("persistence redaction = %q", got)
	}
}

func TestManagedConsoleSessionRejectsInputUntilEnvironmentBootstrapCompletes(t *testing.T) {
	stdin := &recordingWriteCloser{}
	session := &managedConsoleSession{status: "connecting", stdin: stdin}
	if err := session.writeInput("echo unsafe\n"); err == nil {
		t.Fatal("input should be rejected while the session is connecting")
	}
	if stdin.String() != "" {
		t.Fatalf("connecting input reached transport: %q", stdin.String())
	}
	session.status = "connected"
	if err := session.writeInput("echo safe\n"); err != nil {
		t.Fatalf("connected input: %v", err)
	}
	if stdin.String() != "echo safe\n" {
		t.Fatalf("connected input = %q", stdin.String())
	}
}

func TestConsoleSessionManagerEnforcesActiveSessionLimit(t *testing.T) {
	manager := &Manager{sessions: map[int64]*managedConsoleSession{}}
	for i := 0; i < maxActiveConsoleSessions; i++ {
		manager.sessions[int64(i+1)] = &managedConsoleSession{id: int64(i + 1), status: "connected"}
	}

	envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
		Name: "SESSION_LIMIT_TOKEN", Value: []byte("secret-value-for-limit-test"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(context.Background(), CreateRequest{
		RuntimeID: 1, Principal: testExecutionPrincipal(), Environment: envelope,
	}); !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("expected session limit error, got %v", err)
	}
	if err := envelope.WithEntries(func([]sessionenv.EntryView) error { return nil }); !errors.Is(err, sessionenv.ErrDestroyed) {
		t.Fatalf("session limit should destroy the secret environment, got %v", err)
	}
}

func TestManagedConsoleSessionEnforcesClientLimit(t *testing.T) {
	session := &managedConsoleSession{clients: map[*websocket.Conn]*sync.Mutex{}}
	for i := 0; i < maxConsoleClientsPerSession; i++ {
		if _, err := session.addClient(&websocket.Conn{}); err != nil {
			t.Fatalf("add client %d: %v", i, err)
		}
	}
	if _, err := session.addClient(&websocket.Conn{}); !errors.Is(err, ErrClientLimit) {
		t.Fatalf("expected client limit error, got %v", err)
	}
}

func TestConsoleSessionInputLimit(t *testing.T) {
	manager := &Manager{sessions: map[int64]*managedConsoleSession{}}
	if err := manager.Input(context.Background(), testExecutionPrincipal(), 1, strings.Repeat("x", maxConsoleInputBytes+1)); !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("expected input limit error, got %v", err)
	}
}
