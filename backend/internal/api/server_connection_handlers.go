package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/execution"
	"github.com/aipermission/aipermission/backend/internal/servers"
	"github.com/aipermission/aipermission/backend/internal/sshkeys"
)

type serverTestResponse struct {
	ServerID   int64  `json:"server_id"`
	ServerName string `json:"server_name"`
	OK         bool   `json:"ok"`
	Command    string `json:"command"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
}

type serverConnectionTestRequest struct {
	Name                     string `json:"name"`
	Host                     string `json:"host"`
	Port                     int    `json:"port"`
	Username                 string `json:"username"`
	SSHKeyID                 int64  `json:"ssh_key_id"`
	Description              string `json:"description"`
	StartupInputAfterConnect string `json:"startup_input_after_connect"`
	ForceShellCommand        string `json:"force_shell_command"`
}

func (s serverConnectionHandlers) testServer(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	const command = `printf 'aipermission-ok\n'; uname -a`
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	server, privateKey, err := s.serverSSHMaterial(ctx, id)
	if err != nil {
		handleServerSSHMaterialError(w, err)
		return
	}

	result, err := execution.RunCommand(ctx, s.executionTarget(server, privateKey), command)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		writeError(w, http.StatusBadGateway, sshConnectionFailureMessage(err))
		return
	}

	writeJSON(w, http.StatusOK, serverTestResponse{
		ServerID:   server.ID,
		ServerName: server.Name,
		OK:         result.ExitCode == 0,
		Command:    command,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitCode:   result.ExitCode,
		DurationMS: result.DurationMS,
	})
}

func (s serverConnectionHandlers) testServerConnection(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request serverConnectionTestRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.Host = strings.TrimSpace(request.Host)
	request.Username = strings.TrimSpace(request.Username)
	if request.Port == 0 {
		request.Port = 22
	}
	if request.Host == "" {
		writeError(w, http.StatusBadRequest, "host is required")
		return
	}
	if request.Port < 1 || request.Port > 65535 {
		writeError(w, http.StatusBadRequest, "port must be between 1 and 65535")
		return
	}
	if request.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	if request.SSHKeyID < 1 {
		writeError(w, http.StatusBadRequest, "ssh_key_id is required")
		return
	}

	privateKey, err := runtime.sshKeys.GetPrivateKey(r.Context(), request.SSHKeyID)
	if err != nil {
		handleSSHKeyError(w, err)
		return
	}

	const command = `printf 'aipermission-ok\n'; uname -a`
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	result, err := execution.RunCommand(ctx, execution.Target{
		Host:           request.Host,
		Port:           request.Port,
		Username:       request.Username,
		PrivateKey:     privateKey.PrivateKey,
		KnownHostsPath: s.knownHostsPath(),
	}, command)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		writeError(w, http.StatusBadGateway, sshConnectionFailureMessage(err))
		return
	}

	writeJSON(w, http.StatusOK, serverTestResponse{
		OK:         result.ExitCode == 0,
		Command:    command,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitCode:   result.ExitCode,
		DurationMS: result.DurationMS,
	})
}

type consoleExecRequest struct {
	ServerID int64  `json:"server_id"`
	Command  string `json:"command"`
}

type consoleExecResponse struct {
	ServerID   int64  `json:"server_id"`
	ServerName string `json:"server_name"`
	Command    string `json:"command"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
}

func (s serverConnectionHandlers) consoleExec(w http.ResponseWriter, r *http.Request) {
	var request consoleExecRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if request.ServerID < 1 {
		writeError(w, http.StatusBadRequest, "server_id is required")
		return
	}
	if request.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}
	if err := validateTextLimit("command", request.Command, maxCommandBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	server, privateKey, err := s.serverSSHMaterial(ctx, request.ServerID)
	if err != nil {
		handleServerSSHMaterialError(w, err)
		return
	}

	result, err := execution.RunCommand(ctx, s.executionTarget(server, privateKey), request.Command)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		runtime := s.activeRuntime()
		s.writeAudit(r.Context(), runtime, "user", nil, request.ServerID, "console.exec.failed", map[string]any{
			"command": request.Command,
		})
		writeError(w, http.StatusBadGateway, sshCommandFailureMessage(err))
		return
	}
	runtime := s.activeRuntime()
	s.writeAudit(r.Context(), runtime, "user", nil, server.ID, "console.exec.completed", map[string]any{
		"command":     request.Command,
		"exit_code":   result.ExitCode,
		"duration_ms": result.DurationMS,
	})

	writeJSON(w, http.StatusOK, consoleExecResponse{
		ServerID:   server.ID,
		ServerName: server.Name,
		Command:    request.Command,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitCode:   result.ExitCode,
		DurationMS: result.DurationMS,
	})
}

func (s *Server) serverSSHMaterial(ctx context.Context, serverID int64) (servers.Server, sshkeys.PrivateKey, error) {
	runtime := s.activeRuntime()
	if runtime == nil {
		return servers.Server{}, sshkeys.PrivateKey{}, errors.New("database is locked")
	}
	server, err := runtime.servers.Get(ctx, serverID)
	if err != nil {
		return servers.Server{}, sshkeys.PrivateKey{}, err
	}
	privateKey, err := runtime.sshKeys.GetPrivateKey(ctx, server.SSHKeyID)
	if err != nil {
		return servers.Server{}, sshkeys.PrivateKey{}, err
	}
	return server, privateKey, nil
}

func sshConnectionFailureMessage(err error) string {
	return sshFailureMessage("server connection test failed", err)
}

func sshCommandFailureMessage(err error) string {
	return sshFailureMessage("command execution failed", err)
}

func sshFailureMessage(prefix string, err error) string {
	detail := safeSSHErrorDetail(err)
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

func safeSSHErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(err.Error()))
}

func (s *Server) serverSSHMaterialForRuntime(runtime *databaseRuntime) func(context.Context, int64) (servers.Server, sshkeys.PrivateKey, error) {
	return func(ctx context.Context, serverID int64) (servers.Server, sshkeys.PrivateKey, error) {
		server, err := runtime.servers.Get(ctx, serverID)
		if err != nil {
			return servers.Server{}, sshkeys.PrivateKey{}, err
		}
		privateKey, err := runtime.sshKeys.GetPrivateKey(ctx, server.SSHKeyID)
		if err != nil {
			return servers.Server{}, sshkeys.PrivateKey{}, err
		}
		return server, privateKey, nil
	}
}

func (s *Server) executionTarget(server servers.Server, privateKey sshkeys.PrivateKey) execution.Target {
	return execution.Target{
		Host:           server.Host,
		Port:           server.Port,
		Username:       server.Username,
		PrivateKey:     privateKey.PrivateKey,
		KnownHostsPath: s.knownHostsPath(),
	}
}
