package api

import (
	"context"
	"errors"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/execution"
	"github.com/aipermission/aipermission/backend/internal/sshkeys"
)

func (s *Server) serverSSHMaterial(ctx context.Context, serverID int64) (console.Target, sshkeys.PrivateKey, error) {
	runtime := s.activeRuntime()
	if runtime == nil {
		return console.Target{}, sshkeys.PrivateKey{}, errors.New("database is locked")
	}
	return s.serverSSHMaterialFromRuntime(ctx, runtime, serverID)
}

func (s *Server) serverSSHMaterialFromRuntime(ctx context.Context, runtime *databaseRuntime, serverID int64) (console.Target, sshkeys.PrivateKey, error) {
	target, profile, err := connectortargets.NewStore(runtime.database).SSHRuntimeForConsoleID(ctx, serverID)
	if err != nil {
		return console.Target{}, sshkeys.PrivateKey{}, err
	}
	host := strings.TrimSpace(stringConfigValue(target.Config, "host"))
	port := int(int64ConfigValue(target.Config, "port"))
	if port == 0 {
		port = 22
	}
	username := strings.TrimSpace(stringConfigValue(profile.Public, "username"))
	sshKeyID := int64ConfigValue(profile.Public, "ssh_key_id")
	if host == "" || username == "" || sshKeyID < 1 {
		return console.Target{}, sshkeys.PrivateKey{}, errors.New("ssh connector profile is missing host, username, or key")
	}
	privateKey, err := runtime.sshKeys.GetPrivateKey(ctx, sshKeyID)
	if err != nil {
		return console.Target{}, sshkeys.PrivateKey{}, err
	}
	return console.Target{
		ID:                       serverID,
		Name:                     target.Name,
		Host:                     host,
		Port:                     port,
		Username:                 username,
		StartupInputAfterConnect: strings.TrimSpace(stringConfigValue(target.Config, "startup_input_after_connect")),
		ForceShellCommand:        strings.TrimSpace(stringConfigValue(target.Config, "force_shell_command")),
	}, privateKey, nil
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

func (s *Server) serverSSHMaterialForRuntime(runtime *databaseRuntime) func(context.Context, int64) (console.Target, sshkeys.PrivateKey, error) {
	return func(ctx context.Context, serverID int64) (console.Target, sshkeys.PrivateKey, error) {
		return s.serverSSHMaterialFromRuntime(ctx, runtime, serverID)
	}
}

func (s *Server) executionTarget(server console.Target, privateKey sshkeys.PrivateKey) execution.Target {
	return execution.Target{
		Host:           server.Host,
		Port:           server.Port,
		Username:       server.Username,
		PrivateKey:     privateKey.PrivateKey,
		KnownHostsPath: s.knownHostsPath(),
	}
}
