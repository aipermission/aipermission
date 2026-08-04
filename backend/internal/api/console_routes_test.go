package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"golang.org/x/crypto/ssh"
)

func TestMessageAndConsoleRoutes(t *testing.T) {
	fixture := newAPITestFixture(t)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")

	createMessageResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/messages", "", createMessageRequest{TokenID: token.ID, RuntimeID: &server.ID, Message: "hello agent"})
	if createMessageResponse.Code != http.StatusCreated {
		t.Fatalf("create message failed: %d %s", createMessageResponse.Code, createMessageResponse.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/messages?direction=user_to_ai&runtime_id="+strconv.FormatInt(server.ID, 10), "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hello agent") {
		t.Fatalf("list messages failed: %d %s", response.Code, response.Body.String())
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := fixture.db.Exec(`
		INSERT INTO console_sessions (runtime_id, name, status, transcript, cols, rows, created_at, updated_at, closed_at)
		VALUES (?, 'manual', 'closed', 'hello transcript', 120, 32, ?, ?, ?)`,
		server.ID,
		now,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert console session: %v", err)
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("session id: %v", err)
	}
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/console/sessions?runtime_id="+strconv.FormatInt(server.ID, 10), "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hello transcript") {
		t.Fatalf("list console sessions failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/console/sessions/"+strconv.FormatInt(sessionID, 10), "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "manual") {
		t.Fatalf("get console session failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/sessions/"+strconv.FormatInt(sessionID, 10)+"/input", "", console.InputRequest{Data: "ls\n"}); response.Code != http.StatusConflict {
		t.Fatalf("input to inactive session should conflict, got %d", response.Code)
	}
	runningResult, err := fixture.db.Exec(`
		INSERT INTO command_requests (runtime_id, source, command, reason, status, session_id, created_at)
		VALUES (?, 'mcp', 'sleep 60', 'test close cleanup', 'running', ?, ?)`,
		server.ID,
		sessionID,
		now,
	)
	if err != nil {
		t.Fatalf("insert running command request: %v", err)
	}
	runningRequestID, err := runningResult.LastInsertId()
	if err != nil {
		t.Fatalf("running request id: %v", err)
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/sessions/"+strconv.FormatInt(sessionID, 10)+"/close", "", nil); response.Code != http.StatusOK {
		t.Fatalf("close console session failed: %d %s", response.Code, response.Body.String())
	}
	var closedRequestStatus string
	var closedRequestError string
	if err := fixture.db.QueryRow(`SELECT status, error FROM command_requests WHERE id = ?`, runningRequestID).Scan(&closedRequestStatus, &closedRequestError); err != nil {
		t.Fatalf("read closed running request: %v", err)
	}
	if closedRequestStatus != "error" || !strings.Contains(closedRequestError, "console session closed") {
		t.Fatalf("close should mark session running request error, status=%s error=%q", closedRequestStatus, closedRequestError)
	}

	restartServer := fixture.createKeyAndServer(t, "worker-restart")
	restartSessionResult, err := fixture.db.Exec(`
		INSERT INTO console_sessions (runtime_id, name, status, transcript, cols, rows, created_at, updated_at)
		VALUES (?, 'stuck', 'connected', 'stuck transcript', 120, 32, ?, ?)`,
		restartServer.ID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert restart console session: %v", err)
	}
	restartSessionID, err := restartSessionResult.LastInsertId()
	if err != nil {
		t.Fatalf("restart session id: %v", err)
	}
	restartRequestResult, err := fixture.db.Exec(`
		INSERT INTO command_requests (runtime_id, source, command, reason, status, session_id, created_at)
		VALUES (?, 'mcp', 'kubectl get nodes', 'stuck request', 'running', ?, ?)`,
		restartServer.ID,
		restartSessionID,
		now,
	)
	if err != nil {
		t.Fatalf("insert restart running command request: %v", err)
	}
	restartRequestID, err := restartRequestResult.LastInsertId()
	if err != nil {
		t.Fatalf("restart request id: %v", err)
	}
	restartResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/runtime-surfaces/"+strconv.FormatInt(restartServer.ID, 10)+"/restart", "", map[string]any{})
	if restartResponse.Code != http.StatusOK || !strings.Contains(restartResponse.Body.String(), `"status":"restarted"`) || !strings.Contains(restartResponse.Body.String(), `"runtime_id":`) {
		t.Fatalf("restart console session failed: %d %s", restartResponse.Code, restartResponse.Body.String())
	}
	var restartedSessionStatus string
	if err := fixture.db.QueryRow(`SELECT status FROM console_sessions WHERE id = ?`, restartSessionID).Scan(&restartedSessionStatus); err != nil {
		t.Fatalf("read restarted session: %v", err)
	}
	if restartedSessionStatus != "closed" {
		t.Fatalf("expected restarted session closed, got %s", restartedSessionStatus)
	}
	var restartedRequestStatus string
	var restartedRequestError string
	if err := fixture.db.QueryRow(`SELECT status, error FROM command_requests WHERE id = ?`, restartRequestID).Scan(&restartedRequestStatus, &restartedRequestError); err != nil {
		t.Fatalf("read restarted request: %v", err)
	}
	if restartedRequestStatus != "error" || !strings.Contains(restartedRequestError, "restarted by local user") {
		t.Fatalf("restart should mark running request error, status=%s error=%q", restartedRequestStatus, restartedRequestError)
	}

}

func TestCreateConsoleSessionReturnsHostKeyConflict(t *testing.T) {
	fixture := newAPITestFixture(t)
	server := fixture.createKeyAndServer(t, "host-key-change")
	key, err := fixture.sshKeys.Get(context.Background(), server.SSHKeyID)
	if err != nil {
		t.Fatalf("get ssh key: %v", err)
	}
	publicKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key.PublicKey))
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}
	runtime := fixture.server.activeRuntime()
	runtime.consoleSessions = console.NewManager(fixture.db, func(context.Context, console.RuntimeOpenRequest) (*console.RuntimeSession, error) {
		return nil, fmt.Errorf("ssh dial: %w", execution.NewUnknownHostKeyError("[example.test]:22", publicKey))
	}, fixture.server.runtimeRedactor(runtime))

	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/sessions", "", map[string]any{
		"runtime_id":     server.ID,
		"name":           "host-key session",
		"close_existing": true,
	})
	if response.Code != http.StatusConflict {
		t.Fatalf("host key conflict should return 409, got %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "unknown_ssh_host_key") || !strings.Contains(response.Body.String(), `"host_key"`) {
		t.Fatalf("host key conflict should expose structured host_key payload: %s", response.Body.String())
	}
}
