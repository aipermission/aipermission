package postgresconnector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func (Connector) Backup(ctx context.Context, runtime connectors.RuntimeContext, _ connectors.BackupRequest) (connectors.BackupArtifact, error) {
	ctx, cancel := context.WithTimeout(ctx, backupTimeout)
	defer cancel()
	invocation, err := postgresCLIConnection(ctx, runtime)
	if err != nil {
		return connectors.BackupArtifact{}, err
	}
	defer invocation.Cleanup()
	args := invocation.Args
	args = append(args,
		"--format=plain",
		"--clean",
		"--if-exists",
		"--no-owner",
		"--no-privileges",
	)
	var stdout limitedBuffer
	stdout.Limit = maxBackupBytes
	var stderr limitedBuffer
	stderr.Limit = maxRestoreLog
	cmd := exec.CommandContext(ctx, "pg_dump", args...)
	cmd.Env = invocation.Env
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return connectors.BackupArtifact{}, postgresCommandError("pg_dump", err, stderr.String())
	}
	database := targetString(runtime.Target.Config, "database")
	filename := postgresSafeFilename(runtime.Target.Name, database) + ".sql"
	return connectors.BackupArtifact{
		Filename:    filename,
		ContentType: "application/sql; charset=utf-8",
		Data:        stdout.Bytes(),
		Metadata: map[string]any{
			"connector_kind": Kind,
			"database":       database,
			"format":         "plain_sql",
			"clean":          true,
		},
	}, nil
}

func (Connector) Restore(ctx context.Context, runtime connectors.RuntimeContext, request connectors.RestoreRequest) (connectors.ActionResult, error) {
	if request.Content == nil || request.Size == 0 {
		return connectors.ActionResult{}, fmt.Errorf("restore SQL file is empty")
	}
	if request.Size < 0 || request.Size > maxBackupBytes {
		return connectors.ActionResult{}, fmt.Errorf("restore SQL file is too large; maximum restore size is 256 MiB")
	}
	ctx, cancel := context.WithTimeout(ctx, restoreTimeout)
	defer cancel()
	invocation, err := postgresCLIConnection(ctx, runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	defer invocation.Cleanup()
	args := invocation.Args
	args = append(args,
		"--single-transaction",
		"--set", "ON_ERROR_STOP=on",
	)
	var stdout limitedBuffer
	stdout.Limit = maxRestoreLog
	var stderr limitedBuffer
	stderr.Limit = maxRestoreLog
	cmd := exec.CommandContext(ctx, "psql", args...)
	cmd.Env = invocation.Env
	cmd.Stdin = io.LimitReader(request.Content, maxBackupBytes+1)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return connectors.ActionResult{}, postgresCommandError("psql", err, stderr.String())
	}
	output := map[string]any{
		"filename": strings.TrimSpace(request.Filename),
		"stdout":   stdout.String(),
		"stderr":   stderr.String(),
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: "Postgres SQL restore completed",
		Metadata: map[string]any{
			"connector_kind": Kind,
			"database":       targetString(runtime.Target.Config, "database"),
			"filename":       strings.TrimSpace(request.Filename),
		},
	}, nil
}

type postgresCLIInvocation struct {
	Env     []string
	Args    []string
	Cleanup func()
}

func postgresCLIConnection(ctx context.Context, runtime connectors.RuntimeContext) (postgresCLIInvocation, error) {
	username := strings.TrimSpace(publicString(runtime.Profile.Public, "username"))
	if username == "" {
		return postgresCLIInvocation{}, fmt.Errorf("%w: username", ErrMissingSecret)
	}
	if runtime.Secrets == nil {
		return postgresCLIInvocation{}, fmt.Errorf("%w: password", ErrMissingSecret)
	}
	password, err := runtime.Secrets.GetSecret(ctx, "password")
	if err != nil || strings.TrimSpace(password) == "" {
		return postgresCLIInvocation{}, fmt.Errorf("%w: password", ErrMissingSecret)
	}
	host := targetString(runtime.Target.Config, "host")
	database := targetString(runtime.Target.Config, "database")
	if host == "" {
		return postgresCLIInvocation{}, fmt.Errorf("%w: host is required", ErrInvalidConfig)
	}
	if database == "" {
		return postgresCLIInvocation{}, fmt.Errorf("%w: database is required", ErrInvalidConfig)
	}
	port := targetPort(runtime.Target.Config)
	tlsPlan := postgresTLSPlanForTarget(runtime.Target)
	networkHost := ""
	cleanup := func() {}
	if connectionMode(runtime.Target) == "over_ssh" {
		localHost, localPort, stop, err := startPostgresTunnel(ctx, runtime)
		if err != nil {
			return postgresCLIInvocation{}, err
		}
		networkHost = localHost
		port = localPort
		cleanup = stop
	}
	env := append(withoutPostgresEnvironment(os.Environ()),
		"PGPASSWORD="+password,
		"PGSSLMODE="+tlsPlan.Mode,
		"PGCONNECT_TIMEOUT=10",
		"PGAPPNAME=aipermission",
	)
	if tlsPlan.UseSystemRoots {
		env = append(env, "PGSSLROOTCERT=system")
	}
	if networkHost != "" {
		env = append(env, "PGHOSTADDR="+networkHost)
	}
	args := []string{
		"--host", host,
		"--port", strconv.Itoa(port),
		"--username", username,
		"--dbname", database,
		"--no-password",
	}
	return postgresCLIInvocation{Env: env, Args: args, Cleanup: cleanup}, nil
}

func withoutPostgresEnvironment(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(name)), "PG") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

type limitedBuffer struct {
	Limit int
	data  bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Limit <= 0 {
		return len(p), nil
	}
	remaining := b.Limit - b.data.Len()
	if remaining <= 0 {
		return 0, fmt.Errorf("postgres command output exceeded %d bytes", b.Limit)
	}
	if len(p) > remaining {
		_, _ = b.data.Write(p[:remaining])
		return remaining, fmt.Errorf("postgres command output exceeded %d bytes", b.Limit)
	}
	return b.data.Write(p)
}

func (b *limitedBuffer) Bytes() []byte {
	return b.data.Bytes()
}

func (b *limitedBuffer) String() string {
	return b.data.String()
}

func postgresCommandError(command string, err error, stderr string) error {
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return fmt.Errorf("%s is not available in the gateway container; rebuild with postgresql-client installed", command)
	}
	message := strings.TrimSpace(truncateUTF8Bytes(stderr, 4000))
	if message == "" {
		message = err.Error()
	}
	if errors.Is(err, io.ErrShortWrite) {
		message = "command output exceeded gateway limit"
	}
	if command == "pg_dump" && strings.Contains(message, "server version mismatch") {
		message += "\nThe gateway pg_dump client is older than this Postgres server. Rebuild the AIPermission backend image so the bundled Postgres client is updated."
	}
	return fmt.Errorf("%s failed: %s", command, message)
}

func postgresSafeFilename(parts ...string) string {
	candidate := strings.Join(parts, "-")
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	candidate = regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(candidate, "-")
	candidate = strings.Trim(candidate, "-._")
	if candidate == "" {
		return "postgres-backup"
	}
	return candidate
}
