// Package apiadapter registers the SSH connector's gateway adapter.
//
// The generic API package owns routing, auth, permission, approval, history,
// and audit. This package owns SSH-specific runtime behavior.
package apiadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshconfig"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	vaultpkg "github.com/aipermission/aipermission/backend/internal/vault"
	"golang.org/x/crypto/ssh"
)

const (
	initialExecTimeout       = 3 * time.Second
	backgroundCommandTimeout = 30 * time.Minute
	maxConfigParseBytes      = 256 * 1024
)

type LiveConsoleOptions struct {
	ForceShellCommand        string
	StartupInputAfterConnect string
}

type adapter struct{}

func init() {
	connectorapi.Register(sshconnector.Kind, adapter{})
}

func (a adapter) RegisterRoutes(mux connectorapi.RouteMux, server connectorapi.GatewayServer) {
	if mux == nil {
		return
	}
	mux.HandleFunc("POST /api/ssh-host-keys/approve", func(w http.ResponseWriter, r *http.Request) {
		a.approveHostKey(server, w, r)
	})
	mux.HandleFunc("GET /api/ssh-config/discover", func(w http.ResponseWriter, r *http.Request) {
		a.discoverConfig(server, w, r)
	})
	mux.HandleFunc("POST /api/ssh-config/parse", func(w http.ResponseWriter, r *http.Request) {
		a.parseConfig(server, w, r)
	})
}

type sshTargetMaterial struct {
	ID                       int64
	Name                     string
	Host                     string
	Port                     int
	Username                 string
	StartupInputAfterConnect string
	ForceShellCommand        string
}

func (adapter) approveHostKey(server connectorapi.GatewayServer, w http.ResponseWriter, r *http.Request) {
	gateway, err := serverFrom(server)
	if err != nil {
		writeInternalError(w)
		return
	}
	var input hostKeyApprovalRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	input.Host = strings.TrimSpace(input.Host)
	if input.Port == 0 {
		input.Port = 22
	}
	if input.Host == "" {
		writeError(w, http.StatusBadRequest, "host is required")
		return
	}
	if err := validateHost(input.Host); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Port < 1 || input.Port > 65535 {
		writeError(w, http.StatusBadRequest, "port must be between 1 and 65535")
		return
	}
	if strings.TrimSpace(input.PublicKey) == "" {
		writeError(w, http.StatusBadRequest, "public_key is required")
		return
	}
	hostname := net.JoinHostPort(input.Host, strconv.Itoa(input.Port))
	if input.Replace {
		err = execution.ReplaceHostKey(gateway.ConnectorTrustStorePath(), hostname, input.PublicKey)
	} else {
		err = execution.TrustHostKey(gateway.ConnectorTrustStorePath(), hostname, input.PublicKey)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key, err := execution.ParseHostPublicKey(input.PublicKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, hostKeyApprovalResponse{
		Status:            "approved",
		Hostname:          hostname,
		KeyType:           key.Type(),
		FingerprintSHA256: execution.HostKeyFingerprintSHA256(key),
	})
}

func (adapter) discoverConfig(server connectorapi.GatewayServer, w http.ResponseWriter, _ *http.Request) {
	gateway, err := serverFrom(server)
	if err != nil {
		writeInternalError(w)
		return
	}
	if !gateway.ConnectorActiveRuntimeAvailable(w) {
		return
	}
	entries, err := sshconfig.DiscoverDefault()
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read gateway ssh config")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": entries})
}

func (adapter) parseConfig(server connectorapi.GatewayServer, w http.ResponseWriter, r *http.Request) {
	gateway, err := serverFrom(server)
	if err != nil {
		writeInternalError(w)
		return
	}
	if !gateway.ConnectorActiveRuntimeAvailable(w) {
		return
	}
	var input parseConfigRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	input.Content = strings.TrimSpace(input.Content)
	if input.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if len([]byte(input.Content)) > maxConfigParseBytes {
		writeError(w, http.StatusBadRequest, "content is too large")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": sshconfig.Parse(input.Content)})
}

type targetOperationRequest struct {
	ProfileID    int64  `json:"profile_id,omitempty"`
	ContainerRef string `json:"container_ref,omitempty"`
	Tail         int    `json:"tail,omitempty"`
}

type dockerCheckResponse struct {
	RuntimeID  int64                  `json:"runtime_id"`
	TargetName string                 `json:"target_name"`
	Available  bool                   `json:"available"`
	OK         bool                   `json:"ok"`
	Command    string                 `json:"command"`
	Containers []dockerContainerState `json:"containers"`
	Stdout     string                 `json:"stdout"`
	Stderr     string                 `json:"stderr"`
	ExitCode   int                    `json:"exit_code"`
	DurationMS int64                  `json:"duration_ms"`
	CheckedAt  string                 `json:"checked_at"`
}

type dockerContainerState struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Image      string `json:"image"`
	Command    string `json:"command"`
	CreatedAt  string `json:"created_at"`
	Status     string `json:"status"`
	State      string `json:"state"`
	Ports      string `json:"ports"`
	RunningFor string `json:"running_for"`
	Size       string `json:"size"`
	Labels     string `json:"labels"`
	Mounts     string `json:"mounts"`
	Networks   string `json:"networks"`
}

type dockerPSLine struct {
	ID         string `json:"ID"`
	Names      string `json:"Names"`
	Image      string `json:"Image"`
	Command    string `json:"Command"`
	CreatedAt  string `json:"CreatedAt"`
	Status     string `json:"Status"`
	State      string `json:"State"`
	Ports      string `json:"Ports"`
	RunningFor string `json:"RunningFor"`
	Size       string `json:"Size"`
	Labels     string `json:"Labels"`
	Mounts     string `json:"Mounts"`
	Networks   string `json:"Networks"`
}

type dockerLogsResponse struct {
	RuntimeID    int64  `json:"runtime_id"`
	TargetName   string `json:"target_name"`
	ContainerRef string `json:"container_ref"`
	OK           bool   `json:"ok"`
	Command      string `json:"command"`
	Stdout       string `json:"stdout"`
	Stderr       string `json:"stderr"`
	ExitCode     int    `json:"exit_code"`
	DurationMS   int64  `json:"duration_ms"`
	CheckedAt    string `json:"checked_at"`
}

type targetTestResponse struct {
	TargetID      int64          `json:"target_id"`
	ProfileID     int64          `json:"profile_id"`
	ConnectorKind string         `json:"connector_kind"`
	OK            bool           `json:"ok"`
	Status        string         `json:"status"`
	Message       string         `json:"message,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	DurationMS    int64          `json:"duration_ms"`
}

type draftTargetRequest struct {
	Name    string         `json:"name"`
	Config  map[string]any `json:"config,omitempty"`
	Profile map[string]any `json:"profile,omitempty"`
}

type unknownHostKeyResponse struct {
	Error   string            `json:"error"`
	Code    string            `json:"code"`
	HostKey unknownHostKeyDTO `json:"host_key"`
}

type unknownHostKeyDTO struct {
	Host                 string   `json:"host"`
	Port                 int      `json:"port"`
	Hostname             string   `json:"hostname"`
	KeyType              string   `json:"key_type"`
	FingerprintSHA256    string   `json:"fingerprint_sha256"`
	PublicKey            string   `json:"public_key"`
	Changed              bool     `json:"changed,omitempty"`
	ExistingFingerprints []string `json:"existing_fingerprints,omitempty"`
}

type hostKeyApprovalRequest struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	PublicKey string `json:"public_key"`
	Replace   bool   `json:"replace"`
}

type hostKeyApprovalResponse struct {
	Status            string `json:"status"`
	Hostname          string `json:"hostname"`
	KeyType           string `json:"key_type"`
	FingerprintSHA256 string `json:"fingerprint_sha256"`
}

type parseConfigRequest struct {
	Content string `json:"content"`
}

func (adapter) RuntimeCapabilities(server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime) map[string]connectors.RuntimeCapability {
	return map[string]connectors.RuntimeCapability{
		sshconnector.RuntimeServiceName: runtimeExecutor{server: server, runtime: runtime},
	}
}

func (adapter) RuntimeResources(database *sql.DB, secretVault *vaultpkg.Vault) map[string]any {
	if database == nil {
		return nil
	}
	if secretVault == nil {
		return nil
	}
	return map[string]any{
		"keys": sshkeys.NewStore(database, secretVault),
	}
}

func (adapter) WriteConnectorError(w http.ResponseWriter, err error) bool {
	if w == nil {
		return false
	}
	return writeUnknownHostKeyError(w, err)
}

func (adapter) ConnectorErrorMessage(prefix string, err error) string {
	switch strings.TrimSpace(prefix) {
	case "command execution failed":
		return commandFailureMessage(err)
	default:
		return connectionFailureMessage(err)
	}
}

func (adapter) LiveConsoleActionName() string {
	return sshconnector.ActionExec
}

func (adapter) OpenLiveConsole(ctx context.Context, server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, runtimeID int64, rows int, cols int, _ map[string]any) (*console.RuntimeSession, error) {
	gateway, err := serverFrom(server)
	if err != nil {
		return nil, err
	}
	target, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("resolve ssh material: %w", err)
	}
	return openLiveConsoleWithMaterial(ctx, gateway, target, privateKey, rows, cols, LiveConsoleOptions{})
}

func OpenLiveConsoleForTargetRef(ctx context.Context, server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, targetRef string, rows int, cols int, options LiveConsoleOptions) (*console.RuntimeSession, error) {
	gateway, err := serverFrom(server)
	if err != nil {
		return nil, err
	}
	runtimeID, err := runtimeIDForTargetRef(ctx, runtime, targetRef)
	if err != nil {
		return nil, err
	}
	target, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("resolve ssh material: %w", err)
	}
	return openLiveConsoleWithMaterial(ctx, gateway, target, privateKey, rows, cols, options)
}

func openLiveConsoleWithMaterial(_ context.Context, gateway connectorapi.GatewayServer, target sshTargetMaterial, privateKey sshkeys.PrivateKey, rows int, cols int, options LiveConsoleOptions) (*console.RuntimeSession, error) {
	if strings.TrimSpace(options.ForceShellCommand) != "" {
		target.ForceShellCommand = strings.TrimSpace(options.ForceShellCommand)
	}
	if options.StartupInputAfterConnect != "" {
		target.StartupInputAfterConnect = options.StartupInputAfterConnect
	}
	signer, err := ssh.ParsePrivateKey([]byte(privateKey.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	hostKeyCallback, err := execution.HostKeyCallback(gateway.ConnectorTrustStorePath())
	if err != nil {
		return nil, fmt.Errorf("load known_hosts: %w", err)
	}
	config := &ssh.ClientConfig{
		User:            target.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         12 * time.Second,
	}
	address := net.JoinHostPort(target.Host, fmt.Sprintf("%d", target.Port))
	sshClient, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}
	sshSession, err := sshClient.NewSession()
	if err != nil {
		_ = sshClient.Close()
		return nil, fmt.Errorf("new ssh session: %w", err)
	}
	stdin, err := sshSession.StdinPipe()
	if err != nil {
		_ = sshSession.Close()
		_ = sshClient.Close()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := sshSession.StdoutPipe()
	if err != nil {
		_ = sshSession.Close()
		_ = sshClient.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := sshSession.StderrPipe()
	if err != nil {
		_ = sshSession.Close()
		_ = sshClient.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if rows < 1 {
		rows = 32
	}
	if cols < 1 {
		cols = 120
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sshSession.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		_ = sshSession.Close()
		_ = sshClient.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}
	if target.ForceShellCommand != "" {
		if err := sshSession.Start(target.ForceShellCommand); err != nil {
			_ = sshSession.Close()
			_ = sshClient.Close()
			return nil, fmt.Errorf("start forced shell command: %w", err)
		}
	} else if err := sshSession.Shell(); err != nil {
		_ = sshSession.Close()
		_ = sshClient.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}
	return &console.RuntimeSession{
		Stdin:                    stdin,
		Stdout:                   stdout,
		Stderr:                   stderr,
		Wait:                     sshSession.Wait,
		Resize:                   func(cols int, rows int) error { return sshSession.WindowChange(rows, cols) },
		Close:                    func() error { _ = sshSession.Close(); return sshClient.Close() },
		StartupInputAfterConnect: target.StartupInputAfterConnect,
	}, nil
}

func (adapter) DialConnectorTCP(ctx context.Context, server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, targetRef string, network string, address string) (net.Conn, error) {
	if network == "" {
		network = "tcp"
	}
	if network != "tcp" {
		return nil, fmt.Errorf("unsupported SSH connector transport network %q", network)
	}
	gateway, err := serverFrom(server)
	if err != nil {
		return nil, err
	}
	runtimeID, err := runtimeIDForTargetRef(ctx, runtime, targetRef)
	if err != nil {
		return nil, err
	}
	target, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("resolve ssh material: %w", err)
	}
	client, err := execution.DialSSH(ctx, executionTarget(gateway, target, privateKey))
	if err != nil {
		return nil, err
	}
	type response struct {
		conn net.Conn
		err  error
	}
	done := make(chan response, 1)
	go func() {
		conn, err := client.Dial(network, address)
		done <- response{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		_ = client.Close()
		go func() {
			value := <-done
			if value.conn != nil {
				_ = value.conn.Close()
			}
		}()
		return nil, ctx.Err()
	case value := <-done:
		if value.err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("ssh tcp dial: %w", value.err)
		}
		return sshTCPConn{Conn: value.conn, client: client}, nil
	}
}

type sshTCPConn struct {
	net.Conn
	client *ssh.Client
}

func (conn sshTCPConn) Close() error {
	connErr := conn.Conn.Close()
	clientErr := conn.client.Close()
	if connErr != nil {
		return connErr
	}
	return clientErr
}

func (adapter) RunConnectorCommand(ctx context.Context, server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, targetRef string, command string) (connectors.CommandRunResult, error) {
	gateway, err := serverFrom(server)
	if err != nil {
		return connectors.CommandRunResult{}, err
	}
	runtimeID, err := runtimeIDForTargetRef(ctx, runtime, targetRef)
	if err != nil {
		return connectors.CommandRunResult{}, err
	}
	target, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		return connectors.CommandRunResult{}, fmt.Errorf("resolve ssh material: %w", err)
	}
	result, err := execution.RunCommand(ctx, executionTarget(gateway, target, privateKey), command)
	if err != nil {
		return connectors.CommandRunResult{}, err
	}
	return connectors.CommandRunResult{
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitCode:   result.ExitCode,
		DurationMS: result.DurationMS,
	}, nil
}

func (adapter) SupportsRunning(prepared actions.PreparedRequest) bool {
	return prepared.Target.ConnectorKind == sshconnector.Kind && prepared.Action.ActionName == sshconnector.ActionExec
}

func (adapter) RunningHint(request connectortargets.ActionRequest) string {
	if request.ConnectorKind == sshconnector.Kind && request.ActionName == sshconnector.ActionExec {
		return "Wait 3 seconds, then call get_connector_action_request again. For SSH exec actions, inspect live output with the read_console connector action before sending another long-running command to the same target. If the action appears stuck, use the restart_console_session connector action for that target."
	}
	return ""
}

func (adapter) FinishRunning(server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, requestID int64, prepared actions.PreparedRequest) {
	if server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), backgroundCommandTimeout)
	defer cancel()
	runtimeID, resolveErr := runtimeIDForTargetRef(context.Background(), runtime, prepared.Action.TargetRef)
	if resolveErr != nil {
		_, _ = server.ConnectorFinishActionRequest(context.Background(), runtime, requestID, connectors.ResultError, nil, "", resolveErr.Error(), prepared.ActionDefinition.OutputHint)
		return
	}
	sessions, err := consoleSessions(runtime)
	if err != nil {
		_, _ = server.ConnectorFinishActionRequest(context.Background(), runtime, requestID, connectors.ResultError, nil, "", err.Error(), prepared.ActionDefinition.OutputHint)
		return
	}
	result, err := sessions.WaitActive(ctx, runtimeID)
	status := connectors.ResultStatus("")
	var output any
	var displayText string
	var errorText string
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			_ = sessions.InterruptActive(context.Background(), runtimeID)
			status = connectors.ResultError
			errorText = "connector action timed out while running in background"
		} else {
			status = connectors.ResultError
			errorText = err.Error()
		}
	} else {
		status = connectors.ResultCompleted
		if result.ExitCode != 0 {
			status = connectors.ResultFailed
		}
		output = execOutput(result)
		displayText = result.Output
	}
	if status == "" {
		return
	}
	_, _ = server.ConnectorFinishActionRequest(context.Background(), runtime, requestID, status, output, displayText, errorText, prepared.ActionDefinition.OutputHint)
}

type runtimeExecutor struct {
	server  connectorapi.GatewayServer
	runtime connectorapi.GatewayRuntime
}

func (runtimeExecutor) ConnectorRuntimeCapability() string {
	return sshconnector.RuntimeServiceName
}

func (e runtimeExecutor) ExecuteSSHAction(ctx context.Context, _ connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	if e.server == nil || e.runtime == nil {
		return connectors.ActionResult{}, fmt.Errorf("ssh runtime is not available")
	}
	runtimeID, err := runtimeIDForTargetRef(ctx, e.runtime, action.TargetRef)
	if err != nil {
		return connectors.ActionResult{}, err
	}

	switch action.ActionName {
	case sshconnector.ActionExec:
		return e.executeCommand(runtimeID, action)
	case sshconnector.ActionReadConsole:
		return e.readConsole(ctx, runtimeID, action)
	case sshconnector.ActionRestartConsoleSession:
		return e.restartConsole(ctx, runtimeID)
	case sshconnector.ActionBrowseRemoteFiles:
		return e.browseRemoteFiles(ctx, runtimeID, action)
	case sshconnector.ActionStartFileDownload:
		return e.startFileDownload(ctx, runtimeID, action)
	default:
		return connectors.ActionResult{}, fmt.Errorf("%w: %s", sshconnector.ErrUnsupportedAction, action.ActionName)
	}
}

func (e runtimeExecutor) executeCommand(runtimeID int64, action connectors.PreparedAction) (connectors.ActionResult, error) {
	command := stringPayload(action.Payload, "command")
	if command == "" {
		return connectors.ActionResult{}, fmt.Errorf("command is required")
	}
	sessions, err := consoleSessions(e.runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), initialExecTimeout)
	defer cancel()
	result, err := sessions.Exec(ctx, runtimeID, command)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	output := execOutput(result)
	status := connectors.ResultCompleted
	if result.Running {
		status = connectors.ResultRunning
	} else if result.ExitCode != 0 {
		status = connectors.ResultFailed
	}
	response := connectors.ActionResult{
		Status:      status,
		Output:      output,
		DisplayText: output["stdout"].(string),
		Metadata: map[string]any{
			"runtime_id":  runtimeID,
			"duration_ms": result.DurationMS,
		},
		Handles: connectors.ActionHandles{
			SessionID: result.SessionID,
		},
	}
	if result.Running {
		response.Error = "SSH command is still running in the persistent console session."
		response.Handles.FollowupTool = "get_connector_action_request"
	}
	return response, nil
}

func (e runtimeExecutor) readConsole(ctx context.Context, runtimeID int64, action connectors.PreparedAction) (connectors.ActionResult, error) {
	tail := intPayload(action.Payload, "tail_bytes", 20000)
	if tail < 1 {
		tail = 20000
	}
	if tail > 100000 {
		tail = 100000
	}
	sessions, err := consoleSessions(e.runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	items, err := sessions.List(ctx, runtimeID)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if len(items) == 0 {
		return connectors.ActionResult{
			Status: connectors.ResultCompleted,
			Output: map[string]any{
				"runtime_id": runtimeID,
				"status":     "none",
			},
		}, nil
	}
	session := items[0]
	transcript := console.PlainOutput(console.TailStringByBytes(session.Transcript, tail))
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		DisplayText: transcript,
		Output: map[string]any{
			"runtime_id": runtimeID,
			"session_id": session.ID,
			"status":     session.Status,
			"transcript": transcript,
			"error":      session.Error,
			"tail_bytes": tail,
		},
		Handles: connectors.ActionHandles{SessionID: session.ID},
	}, nil
}

func (e runtimeExecutor) restartConsole(ctx context.Context, runtimeID int64) (connectors.ActionResult, error) {
	gateway, err := serverFrom(e.server)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	result, err := gateway.ConnectorRestartConsoleSession(ctx, e.runtime, runtimeID, "console session restarted before connector action completed")
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"runtime_id":                runtimeID,
			"closed_session_ids":        result.ClosedSessionIDs,
			"canceled_running_requests": result.CanceledRunningRequests,
		},
		DisplayText: "SSH console session restarted.",
	}, nil
}

func (e runtimeExecutor) browseRemoteFiles(ctx context.Context, runtimeID int64, action connectors.PreparedAction) (connectors.ActionResult, error) {
	remotePath, err := normalizeRemoteDirectoryPath(stringPayload(action.Payload, "path"))
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if remotePath == "" {
		remotePath = "~"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	gateway, err := serverFrom(e.server)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	target, privateKey, err := targetMaterial(ctx, e.runtime, runtimeID)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	entries, err := execution.ListRemoteDirectory(ctx, executionTarget(gateway, target, privateKey), remotePath)
	if err != nil {
		return connectors.ActionResult{}, fmt.Errorf("%s", connectionFailureMessage(err))
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"runtime_id": runtimeID,
			"path":       remotePath,
			"parent":     browseParent(remotePath),
			"entries":    entries,
		},
	}, nil
}

func (e runtimeExecutor) startFileDownload(ctx context.Context, runtimeID int64, action connectors.PreparedAction) (connectors.ActionResult, error) {
	remotePaths := stringSlicePayload(action.Payload, "remote_paths")
	if len(remotePaths) == 0 {
		return connectors.ActionResult{}, fmt.Errorf("remote_paths is required")
	}
	archiveName := stringPayload(action.Payload, "archive_name")
	gateway, err := serverFrom(e.server)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	batch, err := gateway.ConnectorCreateDownloadBatch(ctx, e.runtime, runtimeID, remotePaths, archiveName, filetransfer.SourceMCP, filetransfer.StatusPending)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	go gateway.ConnectorRunTransferBatch(e.runtime, batch.ID, false)
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"runtime_id": runtimeID,
			"batch_id":   batch.ID,
			"status":     batch.Status,
			"items":      len(batch.Items),
		},
		DisplayText: "SSH download queue started.",
		Handles: connectors.ActionHandles{
			BatchID: batch.ID,
		},
	}, nil
}

func (adapter) BrowseRemoteFiles(ctx context.Context, server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, runtimeID int64, remotePath string) ([]connectorapi.RemoteFileEntry, error) {
	gateway, err := serverFrom(server)
	if err != nil {
		return nil, err
	}
	target, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		return nil, err
	}
	entries, err := execution.ListRemoteDirectory(ctx, executionTarget(gateway, target, privateKey), remotePath)
	if err != nil {
		return nil, err
	}
	return remoteFileEntries(entries), nil
}

func (adapter) StatRemotePath(ctx context.Context, server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, runtimeID int64, remotePath string) (connectorapi.RemotePathStatus, error) {
	gateway, err := serverFrom(server)
	if err != nil {
		return connectorapi.RemotePathStatus{}, err
	}
	target, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		return connectorapi.RemotePathStatus{}, err
	}
	status, err := execution.StatRemotePath(ctx, executionTarget(gateway, target, privateKey), remotePath)
	if err != nil {
		return connectorapi.RemotePathStatus{}, err
	}
	return connectorapi.RemotePathStatus{Exists: status.Exists, Type: status.Type, Size: status.Size}, nil
}

func (adapter) UploadFile(ctx context.Context, server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, runtimeID int64, localPath string, remotePath string, overwrite bool, options connectorapi.TransferOptions) (connectorapi.TransferResult, error) {
	gateway, err := serverFrom(server)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	target, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	result, err := execution.UploadFileWithOptions(ctx, executionTarget(gateway, target, privateKey), localPath, remotePath, overwrite, executionTransferOptions(options))
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	return connectorTransferResult(result), nil
}

func (adapter) DownloadFile(ctx context.Context, server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, runtimeID int64, remotePath string, localPath string, options connectorapi.TransferOptions) (connectorapi.TransferResult, error) {
	gateway, err := serverFrom(server)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	target, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	result, err := execution.DownloadFileWithOptions(ctx, executionTarget(gateway, target, privateKey), remotePath, localPath, executionTransferOptions(options))
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	return connectorTransferResult(result), nil
}

func (adapter) BeforeCreateCredentialProfile(context.Context, connectorapi.GatewayRuntime, *connectortargets.Store, connectortargets.Target) error {
	return nil
}

func (adapter) BeforeDeleteCredentialProfile(ctx context.Context, handler connectorapi.TargetLifecycleGateway, runtime connectorapi.GatewayRuntime, _ *connectortargets.Store, _ connectortargets.Target, profile connectortargets.CredentialProfile) error {
	gateway, err := serverFromHandler(handler)
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runtimeIDs, err := existingLiveConsoleRuntimeIDsForProfile(ctx, runtime, profile.TargetID, profile.ID)
	if err != nil {
		return err
	}
	for _, runtimeID := range runtimeIDs {
		if _, err := gateway.ConnectorRestartConsoleSession(ctx, runtime, runtimeID, "SSH credential profile was deleted before command completed"); err != nil {
			return err
		}
	}
	return nil
}

func (adapter) DeleteTarget(handler connectorapi.TargetLifecycleGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime, target connectortargets.Target) {
	if w == nil || r == nil {
		return
	}
	gateway, err := serverFromHandler(handler)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	database, err := databaseFrom(runtime)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	store := connectortargets.NewStore(database)
	profiles, err := store.ListCredentialProfiles(r.Context(), target.ID)
	if err != nil {
		handleTargetError(w, err)
		return
	}
	removedKeys := int64(0)
	if r.URL.Query().Get("remove_key") == "true" {
		if len(profiles) == 0 {
			writeError(w, http.StatusBadRequest, "remote SSH key cleanup requires a saved credential profile")
			return
		}
		cleanupSeen := map[string]bool{}
		for _, profile := range profiles {
			runtimeID, err := ensureLiveConsoleRuntimeIDForProfile(r.Context(), runtime, target.ID, profile.ID, profile.Label)
			if err != nil {
				handleTargetError(w, err)
				return
			}
			remoteTarget, privateKey, err := targetMaterial(r.Context(), runtime, runtimeID)
			if err != nil {
				handleMaterialError(w, err)
				return
			}
			keyStore, err := keyStore(runtime)
			if err != nil {
				writeInternalError(w)
				return
			}
			sshKeyID := int64ConfigValue(profile.Public, "ssh_key_id")
			key, err := keyStore.Get(r.Context(), sshKeyID)
			if err != nil {
				handleKeyError(w, err)
				return
			}
			cleanupKey := remoteTarget.Username + "\x00" + publicKeyBlob(key.PublicKey)
			if cleanupSeen[cleanupKey] {
				continue
			}
			cleanupSeen[cleanupKey] = true
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			result, err := execution.RunCommand(ctx, executionTarget(gateway, remoteTarget, privateKey), removeAuthorizedKeyCommand(key.PublicKey))
			cancel()
			if err != nil {
				writeError(w, http.StatusBadGateway, "remote key uninstall failed")
				return
			}
			if result.ExitCode != 0 {
				message := strings.TrimSpace(result.Stderr + result.Stdout)
				if message == "" {
					message = "remote key uninstall failed"
				}
				if remoteKeyAlreadyAbsent(message) {
					continue
				}
				writeError(w, http.StatusBadGateway, message)
				return
			}
			removedKeys++
		}
	}
	canceledCommands := int64(0)
	for _, profile := range profiles {
		runtimeIDs, err := existingLiveConsoleRuntimeIDsForProfile(r.Context(), runtime, target.ID, profile.ID)
		if err != nil {
			writeInternalError(w)
			return
		}
		for _, runtimeID := range runtimeIDs {
			result, err := gateway.ConnectorRestartConsoleSession(r.Context(), runtime, runtimeID, "SSH connector target was deleted before command completed")
			if err != nil {
				writeInternalError(w)
				return
			}
			canceledCommands += result.CanceledRunningRequests
		}
	}
	if err := store.DeleteTarget(r.Context(), target.ID); err != nil {
		handleTargetError(w, err)
		return
	}
	if _, err := handler.ConnectorFinalizeDeletedTarget(r.Context(), runtime, target, "SSH connector target was deleted; ask the AI to send a fresh request", map[string]any{
		"remote_key_removed":  removedKeys > 0,
		"remote_keys_removed": removedKeys,
		"canceled_commands":   canceledCommands,
	}); err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "remote_key_removed": removedKeys > 0, "remote_keys_removed": removedKeys})
}

func (adapter) TestCredentialProfile(handler connectorapi.TargetLifecycleGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime, target connectors.TargetView, profile connectors.CredentialProfileView) {
	if w == nil || r == nil {
		return
	}
	gateway, err := serverFromHandler(handler)
	if err != nil {
		writeInternalError(w)
		return
	}
	const command = `printf 'aipermission-ok\n'; uname -a`
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	runtimeID, err := ensureLiveConsoleRuntimeIDForProfile(ctx, runtime, target.ID, profile.ID, profile.Label)
	if err != nil {
		handleTargetError(w, err)
		return
	}
	remoteTarget, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		handleMaterialError(w, err)
		return
	}
	result, err := execution.RunCommand(ctx, executionTarget(gateway, remoteTarget, privateKey), command)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, targetTestResponse{
			TargetID:      target.ID,
			ProfileID:     profile.ID,
			ConnectorKind: target.ConnectorKind,
			OK:            false,
			Status:        "connection_failed",
			Message:       connectionFailureMessage(err),
			DurationMS:    time.Since(start).Milliseconds(),
		})
		return
	}
	writeJSON(w, http.StatusOK, targetTestResponse{
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: target.ConnectorKind,
		OK:            result.ExitCode == 0,
		Status:        "ok",
		Message:       strings.TrimSpace(result.Stderr + result.Stdout),
		Details: map[string]any{
			"command":   command,
			"stdout":    result.Stdout,
			"stderr":    result.Stderr,
			"exit_code": result.ExitCode,
		},
		DurationMS: result.DurationMS,
	})
}

func (adapter) TestDraft(handler connectorapi.TargetLifecycleGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime, requestValue any) {
	if w == nil || r == nil {
		return
	}
	gateway, err := serverFromHandler(handler)
	if err != nil {
		writeInternalError(w)
		return
	}
	draft, err := decodeDraftRequest(requestValue)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload, err := connectorPayload(r.Context(), runtime, draft.Name, draft.Config, draft.Profile)
	if err != nil {
		handleTargetError(w, err)
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	privateKey, err := keyStore.GetPrivateKey(r.Context(), int64ConfigValue(payload.ProfilePublic, "ssh_key_id"))
	if err != nil {
		handleKeyError(w, err)
		return
	}
	const command = `printf 'aipermission-ok\n'; uname -a`
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	result, err := execution.RunCommand(ctx, execution.Target{
		Host:           stringConfigValue(payload.TargetConfig, "host"),
		Port:           intConfigValue(payload.TargetConfig, "port", 22),
		Username:       stringConfigValue(payload.ProfilePublic, "username"),
		PrivateKey:     privateKey.PrivateKey,
		KnownHostsPath: gateway.ConnectorTrustStorePath(),
	}, command)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, targetTestResponse{
			ConnectorKind: sshconnector.Kind,
			OK:            false,
			Status:        "connection_failed",
			Message:       connectionFailureMessage(err),
			DurationMS:    time.Since(start).Milliseconds(),
		})
		return
	}
	writeJSON(w, http.StatusOK, targetTestResponse{
		ConnectorKind: sshconnector.Kind,
		OK:            result.ExitCode == 0,
		Status:        "ok",
		Message:       strings.TrimSpace(result.Stderr + result.Stdout),
		Details: map[string]any{
			"command":   command,
			"stdout":    result.Stdout,
			"stderr":    result.Stderr,
			"exit_code": result.ExitCode,
		},
		DurationMS: result.DurationMS,
	})
}

func (adapter) RunTargetOperation(handler connectorapi.TargetLifecycleGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime, target connectortargets.Target, operation string) {
	if w == nil || r == nil {
		return
	}
	gateway, err := serverFromHandler(handler)
	if err != nil {
		writeInternalError(w)
		return
	}
	var input targetOperationRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	database, err := databaseFrom(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	store := connectortargets.NewStore(database)
	profileID, err := operationProfileID(r.Context(), store, target.ID, input.ProfileID)
	if err != nil {
		handleTargetError(w, err)
		return
	}
	targetRef := connectortargets.TargetProfileRef(sshconnector.Kind, target.ID, profileID)
	runtimeID, err := runtimeIDForTargetRef(r.Context(), runtime, targetRef)
	if err != nil {
		handleTargetError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	remoteTarget, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		handleMaterialError(w, err)
		return
	}
	switch operation {
	case "docker-check":
		response, err := dockerCheckForTarget(ctx, gateway, remoteTarget, privateKey)
		if err != nil {
			if writeUnknownHostKeyError(w, err) {
				return
			}
			writeError(w, http.StatusBadGateway, commandFailureMessage(err))
			return
		}
		handler.ConnectorWriteAudit(r.Context(), runtime, "user", nil, remoteTarget.ID, "server.docker_check", map[string]any{
			"available":  response.Available,
			"exit_code":  response.ExitCode,
			"containers": len(response.Containers),
		})
		writeJSON(w, http.StatusOK, response)
	case "docker-logs":
		containerRef := strings.TrimSpace(input.ContainerRef)
		if err := validateDockerContainerRef(containerRef); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := dockerLogsForTarget(ctx, gateway, remoteTarget, privateKey, containerRef, input.Tail)
		if err != nil {
			if writeUnknownHostKeyError(w, err) {
				return
			}
			writeError(w, http.StatusBadGateway, commandFailureMessage(err))
			return
		}
		handler.ConnectorWriteAudit(r.Context(), runtime, "user", nil, remoteTarget.ID, "server.docker_logs", map[string]any{
			"container_ref": containerRef,
			"exit_code":     response.ExitCode,
			"tail":          normalizeDockerLogsTail(input.Tail),
		})
		writeJSON(w, http.StatusOK, response)
	default:
		writeError(w, http.StatusBadRequest, "unsupported connector operation")
	}
}

func (adapter) CanonicalCredentialPublic(ctx context.Context, _ connectorapi.TargetLifecycleGateway, runtime connectorapi.GatewayRuntime, credentialKind string, public map[string]any) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return canonicalCredentialPublic(ctx, runtime, credentialKind, public)
}

func dockerCheckForTarget(ctx context.Context, gateway connectorapi.GatewayServer, target sshTargetMaterial, privateKey sshkeys.PrivateKey) (dockerCheckResponse, error) {
	const command = `if ! command -v docker >/dev/null 2>&1; then
  printf '__AIPERMISSION_DOCKER_UNAVAILABLE__\n'
  exit 0
fi
docker ps --format '{{json .}}'`
	result, err := execution.RunCommand(ctx, executionTarget(gateway, target, privateKey), command)
	if err != nil {
		return dockerCheckResponse{}, err
	}
	containers, available := parseDockerPSOutput(result.Stdout)
	return dockerCheckResponse{
		RuntimeID:  target.ID,
		TargetName: target.Name,
		Available:  available,
		OK:         available && result.ExitCode == 0,
		Command:    command,
		Containers: containers,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitCode:   result.ExitCode,
		DurationMS: result.DurationMS,
		CheckedAt:  time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func dockerLogsForTarget(ctx context.Context, gateway connectorapi.GatewayServer, target sshTargetMaterial, privateKey sshkeys.PrivateKey, containerRef string, tailValue int) (dockerLogsResponse, error) {
	tail := normalizeDockerLogsTail(tailValue)
	command := fmt.Sprintf(`if ! command -v docker >/dev/null 2>&1; then
  printf 'docker command is not available\n' >&2
  exit 127
fi
docker logs --tail %s --timestamps %s`, strconv.Itoa(tail), shellQuote(containerRef))
	result, err := execution.RunCommand(ctx, executionTarget(gateway, target, privateKey), command)
	if err != nil {
		return dockerLogsResponse{}, err
	}
	return dockerLogsResponse{
		RuntimeID:    target.ID,
		TargetName:   target.Name,
		ContainerRef: containerRef,
		OK:           result.ExitCode == 0,
		Command:      command,
		Stdout:       result.Stdout,
		Stderr:       result.Stderr,
		ExitCode:     result.ExitCode,
		DurationMS:   result.DurationMS,
		CheckedAt:    time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (adapter) LiveConsoleCapabilityKind() string {
	return connectortargets.RuntimeCapabilityLiveConsole
}

func (adapter) LiveConsoleTargetRef(ctx context.Context, runtime connectorapi.GatewayRuntime, runtimeID int64) (string, error) {
	contextValue, _ := ctx.(context.Context)
	if contextValue == nil {
		contextValue = context.Background()
	}
	database, err := databaseFrom(runtime)
	if err != nil {
		return "", err
	}
	target, profile, surface, err := connectortargets.NewStore(database).TargetProfileByRuntimeID(contextValue, runtimeID)
	if err != nil {
		return "", err
	}
	if surface.ConnectorKind != sshconnector.Kind || surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return "", connectortargets.ErrRuntimeSurfaceNotFound
	}
	return connectortargets.ConnectorTargetRef(target.ConnectorKind, target.ID, profile.ID), nil
}

func (adapter) ResolveLiveConsoleMaterial(ctx context.Context, runtime connectorapi.GatewayRuntime, runtimeID int64) (any, any, error) {
	contextValue, _ := ctx.(context.Context)
	if contextValue == nil {
		contextValue = context.Background()
	}
	target, privateKey, err := targetMaterial(contextValue, runtime, runtimeID)
	if err != nil {
		return nil, nil, err
	}
	return target, privateKey, nil
}

func (adapter) LiveConsoleTargetMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
	metadata := map[string]any{}
	if host := stringConfigValue(target.Config, "host"); host != "" {
		metadata["host"] = host
	}
	if port := intConfigValue(target.Config, "port", 22); port > 0 {
		metadata["port"] = port
	}
	if username := stringConfigValue(profile.Public, "username"); username != "" {
		metadata["username"] = username
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func (adapter) ListCredentialResources(_ connectorapi.CredentialResourceGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime) {
	if w == nil || r == nil {
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	items, err := keyStore.List(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (adapter) CreateCredentialResource(_ connectorapi.CredentialResourceGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime) {
	if w == nil || r == nil {
		return
	}
	var input sshkeys.CreateRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	item, err := keyStore.Create(r.Context(), input)
	if err != nil {
		handleKeyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (adapter) ImportCredentialResource(_ connectorapi.CredentialResourceGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime) {
	if w == nil || r == nil {
		return
	}
	var input sshkeys.ImportRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	item, err := keyStore.Import(r.Context(), input)
	if err != nil {
		handleKeyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (adapter) GetCredentialResource(_ connectorapi.CredentialResourceGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime) {
	if w == nil || r == nil {
		return
	}
	id, ok := parsePathID(w, r)
	if !ok {
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	item, err := keyStore.Get(r.Context(), id)
	if err != nil {
		handleKeyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (adapter) UpdateCredentialResource(_ connectorapi.CredentialResourceGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime) {
	if w == nil || r == nil {
		return
	}
	id, ok := parsePathID(w, r)
	if !ok {
		return
	}
	var input sshkeys.UpdateRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	item, err := keyStore.Update(r.Context(), id, input)
	if err != nil {
		handleKeyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (adapter) DeleteCredentialResource(_ connectorapi.CredentialResourceGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime) {
	if w == nil || r == nil {
		return
	}
	id, ok := parsePathID(w, r)
	if !ok {
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := keyStore.Delete(r.Context(), id); err != nil {
		handleKeyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type connectorPayloadValue struct {
	Name          string
	TargetConfig  map[string]any
	ProfileLabel  string
	ProfilePublic map[string]any
}

func connectorPayload(ctx context.Context, runtime connectorapi.GatewayRuntime, name string, config map[string]any, profile map[string]any) (connectorPayloadValue, error) {
	if config == nil {
		config = map[string]any{}
	}
	if profile == nil {
		profile = map[string]any{}
	}
	targetConfig, err := targetConfigFromConnectorConfig(config)
	if err != nil {
		return connectorPayloadValue{}, err
	}
	sshConnector := sshconnector.New()
	if err := connectors.ValidateNonSecretSchema(sshConnector.TargetSchema(), "ssh target"); err != nil {
		return connectorPayloadValue{}, err
	}
	if err := connectors.ValidateSchemaValues(sshConnector.TargetSchema(), targetConfig); err != nil {
		return connectorPayloadValue{}, err
	}
	username := stringConfigValue(profile, "username")
	if username == "" {
		return connectorPayloadValue{}, connectortargets.ValidationError("username is required")
	}
	profilePublic, err := canonicalCredentialPublic(ctx, runtime, "private_key", map[string]any{
		"username":   username,
		"ssh_key_id": profile["ssh_key_id"],
	})
	if err != nil {
		return connectorPayloadValue{}, err
	}
	return connectorPayloadValue{
		Name:          strings.TrimSpace(name),
		TargetConfig:  targetConfig,
		ProfileLabel:  strings.TrimSpace(username),
		ProfilePublic: profilePublic,
	}, nil
}

func canonicalCredentialPublic(ctx context.Context, runtime connectorapi.GatewayRuntime, credentialKind string, public map[string]any) (map[string]any, error) {
	if strings.TrimSpace(credentialKind) != "private_key" {
		return nil, connectortargets.ValidationError("unsupported SSH credential kind")
	}
	username := stringConfigValue(public, "username")
	if username == "" {
		return nil, connectortargets.ValidationError("username is required")
	}
	keyID := int64ConfigValue(public, "ssh_key_id")
	if keyID < 1 {
		return nil, connectortargets.ValidationError("ssh_key_id is required")
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		return nil, err
	}
	key, err := keyStore.Get(ctx, keyID)
	if err != nil {
		if errors.Is(err, sshkeys.ErrNotFound) {
			return nil, connectortargets.ValidationError("ssh_key_id does not reference an existing SSH credential")
		}
		return nil, err
	}
	return map[string]any{
		"username":    username,
		"ssh_key_id":  key.ID,
		"key_name":    key.Name,
		"key_type":    key.KeyType,
		"fingerprint": key.Fingerprint,
	}, nil
}

func targetConfigFromConnectorConfig(config map[string]any) (map[string]any, error) {
	allowed := map[string]bool{
		"host":                        true,
		"port":                        true,
		"description":                 true,
		"startup_input_after_connect": true,
		"force_shell_command":         true,
	}
	for key := range config {
		if !allowed[key] {
			return nil, connectortargets.ValidationError("unsupported SSH connector field " + key)
		}
	}
	return map[string]any{
		"host":                        config["host"],
		"port":                        config["port"],
		"description":                 config["description"],
		"startup_input_after_connect": config["startup_input_after_connect"],
		"force_shell_command":         config["force_shell_command"],
	}, nil
}

func runtimeIDForTargetRef(ctx context.Context, runtime connectorapi.GatewayRuntime, targetRef string) (int64, error) {
	database, err := databaseFrom(runtime)
	if err != nil {
		return 0, err
	}
	targetID, profileID, ok := connectortargets.ParseTargetProfileRef(sshconnector.Kind, targetRef)
	if !ok {
		return 0, connectortargets.ErrInvalidTargetRef
	}
	store := connectortargets.NewStore(database)
	target, profile, err := store.ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return 0, err
	}
	surface, err := store.EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind:  sshconnector.Kind,
		TargetID:       targetID,
		ProfileID:      profileID,
		CapabilityKind: connectortargets.RuntimeCapabilityLiveConsole,
		Label:          profile.Label,
	})
	if err != nil {
		return 0, err
	}
	if surface.TargetID != target.ID || surface.ProfileID != profile.ID {
		return 0, connectortargets.ErrRuntimeSurfaceNotFound
	}
	return surface.ID, nil
}

func ensureLiveConsoleRuntimeIDForProfile(ctx context.Context, runtime connectorapi.GatewayRuntime, targetID int64, profileID int64, label string) (int64, error) {
	database, err := databaseFrom(runtime)
	if err != nil {
		return 0, err
	}
	surface, err := connectortargets.NewStore(database).EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind:  sshconnector.Kind,
		TargetID:       targetID,
		ProfileID:      profileID,
		CapabilityKind: connectortargets.RuntimeCapabilityLiveConsole,
		Label:          label,
	})
	if err != nil {
		return 0, err
	}
	return surface.ID, nil
}

func existingLiveConsoleRuntimeIDsForProfile(ctx context.Context, runtime connectorapi.GatewayRuntime, targetID int64, profileID int64) ([]int64, error) {
	database, err := databaseFrom(runtime)
	if err != nil {
		return nil, err
	}
	surfaces, err := connectortargets.NewStore(database).ListRuntimeSurfacesForProfile(ctx, targetID, profileID, connectortargets.RuntimeCapabilityLiveConsole)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(surfaces))
	for _, surface := range surfaces {
		if surface.ConnectorKind == sshconnector.Kind {
			ids = append(ids, surface.ID)
		}
	}
	return ids, nil
}

func targetMaterial(ctx context.Context, runtime connectorapi.GatewayRuntime, runtimeID int64) (sshTargetMaterial, sshkeys.PrivateKey, error) {
	database, err := databaseFrom(runtime)
	if err != nil {
		return sshTargetMaterial{}, sshkeys.PrivateKey{}, err
	}
	target, profile, surface, err := connectortargets.NewStore(database).TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return sshTargetMaterial{}, sshkeys.PrivateKey{}, err
	}
	if surface.ConnectorKind != sshconnector.Kind || surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole {
		return sshTargetMaterial{}, sshkeys.PrivateKey{}, connectortargets.ErrRuntimeSurfaceNotFound
	}
	host := strings.TrimSpace(stringConfigValue(target.Config, "host"))
	port := intConfigValue(target.Config, "port", 22)
	username := strings.TrimSpace(stringConfigValue(profile.Public, "username"))
	keyID := int64ConfigValue(profile.Public, "ssh_key_id")
	if host == "" || username == "" || keyID < 1 {
		return sshTargetMaterial{}, sshkeys.PrivateKey{}, errors.New("ssh connector profile is missing host, username, or key")
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		return sshTargetMaterial{}, sshkeys.PrivateKey{}, err
	}
	privateKey, err := keyStore.GetPrivateKey(ctx, keyID)
	if err != nil {
		return sshTargetMaterial{}, sshkeys.PrivateKey{}, err
	}
	return sshTargetMaterial{
		ID:                       runtimeID,
		Name:                     target.Name,
		Host:                     host,
		Port:                     port,
		Username:                 username,
		StartupInputAfterConnect: strings.TrimSpace(stringConfigValue(target.Config, "startup_input_after_connect")),
		ForceShellCommand:        strings.TrimSpace(stringConfigValue(target.Config, "force_shell_command")),
	}, privateKey, nil
}

func databaseFrom(runtime connectorapi.GatewayRuntime) (*sql.DB, error) {
	if runtime == nil || runtime.ConnectorDatabase() == nil {
		return nil, fmt.Errorf("database runtime is not available")
	}
	return runtime.ConnectorDatabase(), nil
}

func keyStore(runtime connectorapi.GatewayRuntime) (*sshkeys.Store, error) {
	if runtime == nil {
		return nil, fmt.Errorf("ssh key store is not available")
	}
	store, ok := runtime.ConnectorResource(sshconnector.Kind, "keys").(*sshkeys.Store)
	if !ok || store == nil {
		return nil, fmt.Errorf("ssh key store is not available")
	}
	return store, nil
}

func consoleSessions(runtime connectorapi.GatewayRuntime) (*console.Manager, error) {
	if runtime == nil || runtime.ConnectorConsoleSessions() == nil {
		return nil, fmt.Errorf("ssh console runtime is not available")
	}
	return runtime.ConnectorConsoleSessions(), nil
}

func executionTarget(gateway connectorapi.GatewayServer, target sshTargetMaterial, privateKey sshkeys.PrivateKey) execution.Target {
	return execution.Target{
		Host:           target.Host,
		Port:           target.Port,
		Username:       target.Username,
		PrivateKey:     privateKey.PrivateKey,
		KnownHostsPath: gateway.ConnectorTrustStorePath(),
	}
}

func executionTransferOptions(options connectorapi.TransferOptions) execution.TransferOptions {
	return execution.TransferOptions{
		Progress: func(transferred int64, total int64) {
			if options.Progress != nil {
				options.Progress(transferred, total)
			}
		},
		Wait: options.Wait,
	}
}

func connectorTransferResult(result execution.TransferResult) connectorapi.TransferResult {
	return connectorapi.TransferResult{
		Bytes:          result.Bytes,
		Size:           result.Size,
		ChecksumSHA256: result.ChecksumSHA256,
		DurationMS:     result.DurationMS,
	}
}

func remoteFileEntries(entries []execution.RemoteFileEntry) []connectorapi.RemoteFileEntry {
	items := make([]connectorapi.RemoteFileEntry, 0, len(entries))
	for _, entry := range entries {
		items = append(items, connectorapi.RemoteFileEntry{
			Name:       entry.Name,
			Path:       entry.Path,
			Type:       entry.Type,
			Size:       entry.Size,
			ModifiedAt: entry.ModifiedAt,
		})
	}
	return items
}

func serverFrom(value connectorapi.GatewayServer) (connectorapi.GatewayServer, error) {
	if value == nil {
		return nil, fmt.Errorf("gateway services are not available")
	}
	return value, nil
}

func serverFromHandler(value connectorapi.TargetLifecycleGateway) (connectorapi.GatewayServer, error) {
	if value == nil {
		return nil, fmt.Errorf("gateway handler services are not available")
	}
	return serverFrom(value.ConnectorServer())
}

func decodeDraftRequest(value any) (draftTargetRequest, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return draftTargetRequest{}, fmt.Errorf("invalid connector draft request")
	}
	var request draftTargetRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return draftTargetRequest{}, fmt.Errorf("invalid connector draft request")
	}
	return request, nil
}

func operationProfileID(ctx context.Context, store *connectortargets.Store, targetID int64, requestedProfileID int64) (int64, error) {
	if requestedProfileID > 0 {
		return requestedProfileID, nil
	}
	profiles, err := store.ListCredentialProfiles(ctx, targetID)
	if err != nil {
		return 0, err
	}
	if len(profiles) == 0 {
		return 0, connectortargets.ErrTargetProfileNotFound
	}
	if len(profiles) > 1 {
		return 0, connectortargets.ValidationError("profile_id is required when an SSH connector target has multiple credential profiles")
	}
	return profiles[0].ID, nil
}

func execOutput(result console.ExecResult) map[string]any {
	return map[string]any{
		"command":     result.Command,
		"stdout":      console.PlainOutput(result.Output),
		"stderr":      "",
		"exit_code":   result.ExitCode,
		"running":     result.Running,
		"session_id":  result.SessionID,
		"duration_ms": result.DurationMS,
	}
}

func stringPayload(payload map[string]any, name string) string {
	value, ok := payload[name]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func intPayload(payload map[string]any, name string, fallback int) int {
	value, ok := payload[name]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return fallback
	}
}

func stringSlicePayload(payload map[string]any, name string) []string {
	value, ok := payload[name]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		return out
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	default:
		return []string{fmt.Sprint(typed)}
	}
}

func browseParent(remotePath string) string {
	if remotePath == "" || remotePath == "/" || remotePath == "." {
		return "/"
	}
	parent := path.Dir(remotePath)
	if parent == "." {
		return "/"
	}
	return parent
}

func normalizeRemoteDirectoryPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("path cannot contain control characters")
	}
	return value, nil
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeInternalError(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func handleTargetError(w http.ResponseWriter, err error) {
	var validation connectortargets.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Error())
	case errors.Is(err, connectortargets.ErrTargetNotFound), errors.Is(err, connectortargets.ErrTargetProfileNotFound):
		writeError(w, http.StatusNotFound, "connector target profile not found")
	default:
		writeInternalError(w)
	}
}

func handleKeyError(w http.ResponseWriter, err error) {
	var validation sshkeys.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Error())
	case errors.Is(err, sshkeys.ErrNotFound):
		writeError(w, http.StatusNotFound, "ssh key not found")
	default:
		writeInternalError(w)
	}
}

func handleMaterialError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, connectortargets.ErrTargetProfileNotFound), errors.Is(err, connectortargets.ErrTargetNotFound):
		writeError(w, http.StatusNotFound, "connector target profile not found")
	case errors.Is(err, sshkeys.ErrNotFound):
		handleKeyError(w, err)
	default:
		writeInternalError(w)
	}
}

func connectionFailureMessage(err error) string {
	return failureMessage("server connection test failed", err)
}

func commandFailureMessage(err error) string {
	return failureMessage("command execution failed", err)
}

func failureMessage(prefix string, err error) string {
	detail := safeErrorDetail(err)
	switch {
	case detail == "":
		return prefix
	case strings.Contains(detail, "unable to authenticate") || strings.Contains(detail, "no supported methods remain") || strings.Contains(detail, "permission denied"):
		return prefix + ": SSH authentication failed. Install the selected SSH public key on the server, then try again."
	case strings.Contains(detail, "connection refused"):
		return prefix + ": SSH port refused the connection. Check the host, port, and SSH service."
	case strings.Contains(detail, "i/o timeout") || strings.Contains(detail, "timed out") || strings.Contains(detail, "deadline exceeded"):
		return prefix + ": SSH connection timed out. Check network access, firewall rules, host, and port."
	case strings.Contains(detail, "no route to host") || strings.Contains(detail, "network is unreachable"):
		return prefix + ": the host is not reachable from the local gateway."
	case strings.Contains(detail, "host key"):
		return prefix + ": SSH host key verification failed."
	case strings.Contains(detail, "parse private key"):
		return prefix + ": selected SSH key could not be parsed."
	default:
		return prefix + ": " + detail
	}
}

func safeErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(err.Error()))
}

func writeUnknownHostKeyError(w http.ResponseWriter, err error) bool {
	var unknown *execution.UnknownHostKeyError
	if errors.As(err, &unknown) {
		writeHostKeyConflict(w, "ssh host key approval required", "unknown_ssh_host_key", unknownHostKeyDTOFromUnknown(unknown))
		return true
	}
	var changed *execution.ChangedHostKeyError
	if errors.As(err, &changed) {
		writeHostKeyConflict(w, "ssh host key changed; replace trusted fingerprint only if this change is expected", "changed_ssh_host_key", unknownHostKeyDTOFromChanged(changed))
		return true
	}
	return false
}

func writeHostKeyConflict(w http.ResponseWriter, errorMessage string, code string, hostKey unknownHostKeyDTO) {
	writeJSON(w, http.StatusConflict, unknownHostKeyResponse{
		Error:   errorMessage,
		Code:    code,
		HostKey: hostKey,
	})
}

func unknownHostKeyDTOFromUnknown(err *execution.UnknownHostKeyError) unknownHostKeyDTO {
	host, port := splitHostKeyHostPort(err.Hostname)
	return unknownHostKeyDTO{
		Host:              host,
		Port:              port,
		Hostname:          err.Hostname,
		KeyType:           err.KeyType,
		FingerprintSHA256: err.FingerprintSHA256,
		PublicKey:         err.PublicKey,
	}
}

func unknownHostKeyDTOFromChanged(err *execution.ChangedHostKeyError) unknownHostKeyDTO {
	host, port := splitHostKeyHostPort(err.Hostname)
	return unknownHostKeyDTO{
		Host:                 host,
		Port:                 port,
		Hostname:             err.Hostname,
		KeyType:              err.KeyType,
		FingerprintSHA256:    err.FingerprintSHA256,
		PublicKey:            err.PublicKey,
		Changed:              true,
		ExistingFingerprints: err.ExistingFingerprints,
	}
}

func splitHostKeyHostPort(hostname string) (string, int) {
	host, portText, splitErr := net.SplitHostPort(hostname)
	if splitErr != nil {
		return hostname, 22
	}
	port, parseErr := strconv.Atoi(portText)
	if parseErr != nil || port < 1 {
		port = 22
	}
	return host, port
}

func validateHost(host string) error {
	if len([]rune(host)) > 255 {
		return connectortargets.ValidationError("host must be 255 characters or fewer")
	}
	if strings.Contains(host, "://") || strings.ContainsAny(host, "/\\") {
		return connectortargets.ValidationError("host must be a hostname or IP address, not a URL")
	}
	if strings.ContainsAny(host, " \t\r\n") {
		return connectortargets.ValidationError("host cannot contain whitespace")
	}
	for _, r := range host {
		if unicode.IsControl(r) {
			return connectortargets.ValidationError("host cannot contain control characters")
		}
	}
	return nil
}

func validateDockerContainerRef(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("container is required")
	}
	if len(value) > 128 {
		return fmt.Errorf("container must be 128 characters or fewer")
	}
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\x00' {
			return fmt.Errorf("container cannot contain control characters")
		}
	}
	return nil
}

func normalizeDockerLogsTail(value int) int {
	if value <= 0 {
		return 300
	}
	if value > 5000 {
		return 5000
	}
	return value
}

func parseDockerPSOutput(output string) ([]dockerContainerState, bool) {
	output = strings.TrimSpace(output)
	if output == "" {
		return []dockerContainerState{}, true
	}
	if strings.Contains(output, "__AIPERMISSION_DOCKER_UNAVAILABLE__") {
		return []dockerContainerState{}, false
	}
	containers := []dockerContainerState{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var parsed dockerPSLine
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			continue
		}
		containers = append(containers, dockerContainerState{
			ID:         parsed.ID,
			Name:       parsed.Names,
			Image:      parsed.Image,
			Command:    parsed.Command,
			CreatedAt:  parsed.CreatedAt,
			Status:     parsed.Status,
			State:      parsed.State,
			Ports:      parsed.Ports,
			RunningFor: parsed.RunningFor,
			Size:       parsed.Size,
			Labels:     parsed.Labels,
			Mounts:     parsed.Mounts,
			Networks:   parsed.Networks,
		})
	}
	return containers, true
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func remoteKeyAlreadyAbsent(message string) bool {
	return strings.Contains(message, "remote key uninstall removed 0 authorized_keys entries")
}

func removeAuthorizedKeyCommand(publicKey string) string {
	blob := publicKeyBlob(publicKey)
	delimiter := "__AIPERMISSION_AUTHORIZED_KEY__"
	for strings.Contains(blob, "\n"+delimiter+"\n") {
		delimiter += "_X"
	}
	return `set -e
KEY_BLOB="$(cat <<'` + delimiter + `'
` + blob + `
` + delimiter + `
)"
if [ -z "$KEY_BLOB" ]; then
  echo "remote key uninstall failed: invalid public key" >&2
  exit 1
fi
mkdir -p ~/.ssh
touch ~/.ssh/authorized_keys
chmod 700 ~/.ssh
tmp="$HOME/.ssh/authorized_keys.aipermission.$$"
awk -v key_blob="$KEY_BLOB" '
BEGIN { removed = 0 }
{
  keep = 1
  for (i = 1; i <= NF; i++) {
    if ($i == key_blob) {
      keep = 0
      removed++
      break
    }
  }
  if (keep) print
}
END { print removed > "/dev/stderr" }
' ~/.ssh/authorized_keys 2>"$tmp.count" > "$tmp"
removed="$(cat "$tmp.count" 2>/dev/null || printf '0')"
rm -f "$tmp.count"
if [ "${removed:-0}" -eq 0 ]; then
  rm -f "$tmp"
  echo "remote key uninstall removed 0 authorized_keys entries" >&2
  exit 1
fi
cat "$tmp" > ~/.ssh/authorized_keys
rm -f "$tmp"
chmod 600 ~/.ssh/authorized_keys
printf 'aipermission_key_removed=%s\n' "$removed"`
}

func publicKeyBlob(publicKey string) string {
	fields := strings.Fields(publicKey)
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func parsePathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	idText := strings.TrimSpace(r.PathValue("id"))
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func stringConfigValue(config map[string]any, key string) string {
	value, ok := config[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func intConfigValue(config map[string]any, key string, fallback int) int {
	value, ok := config[key]
	if !ok || value == nil {
		return fallback
	}
	parsed, ok := nativeIntValue(value)
	if !ok || parsed == 0 {
		return fallback
	}
	return parsed
}

func nativeIntValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		if !int64FitsNativeInt(typed) {
			return 0, false
		}
		return int(typed), true
	case float64:
		if typed != math.Trunc(typed) || !float64FitsNativeInt(typed) {
			return 0, false
		}
		return int(typed), true
	case json.Number:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed.String()), 10, strconv.IntSize)
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, strconv.IntSize)
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

func int64FitsNativeInt(value int64) bool {
	if strconv.IntSize == 32 {
		return value >= -1<<31 && value <= 1<<31-1
	}
	return true
}

func float64FitsNativeInt(value float64) bool {
	if strconv.IntSize == 32 {
		return value >= float64(-1<<31) && value <= float64(1<<31-1)
	}
	return value >= float64(-1<<63) && value <= float64(1<<63-1)
}

func int64ConfigValue(config map[string]any, key string) int64 {
	value, ok := config[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}
