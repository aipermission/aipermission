package postgresconnector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func postgresBackupCommand(ctx context.Context, invocation postgresCLIInvocation) (string, int, error) {
	args := append(append([]string(nil), invocation.Args...),
		"--no-psqlrc", "--no-align", "--tuples-only", "--set", "ON_ERROR_STOP=on",
		"--command", "SHOW server_version_num")
	output, err := postgresClientOutput(ctx, "psql", args, invocation.Env)
	if err != nil {
		return "", 0, fmt.Errorf("inspect Postgres backup server version: %w", err)
	}
	version, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil || version < 100000 || version >= 1000000 {
		return "", 0, fmt.Errorf("invalid Postgres backup server version")
	}
	major := version / 10000
	command, err := matchingPostgresDump(ctx, major, exec.LookPath)
	return command, major, err
}

func matchingPostgresDump(ctx context.Context, major int, lookup func(string) (string, error)) (string, error) {
	// Newer pg_dump output is not guaranteed to restore to an older server.
	command, err := lookup(fmt.Sprintf("/usr/lib/postgresql/%d/bin/pg_dump", major))
	if err != nil {
		command, err = lookup("pg_dump")
	}
	if err != nil {
		return "", fmt.Errorf("pg_dump %d is required for this server; install postgresql-client-%d in the gateway", major, major)
	}
	output, err := postgresClientOutput(ctx, command, []string{"--version"}, withoutPostgresEnvironment(os.Environ()))
	if err != nil {
		return "", fmt.Errorf("verify Postgres backup client version: %w", err)
	}
	fields := strings.Fields(output)
	if len(fields) < 3 || fields[0] != "pg_dump" || fields[1] != "(PostgreSQL)" {
		return "", fmt.Errorf("invalid pg_dump version response")
	}
	version, _, _ := strings.Cut(fields[2], ".")
	clientMajor, err := strconv.Atoi(version)
	if err != nil || clientMajor != major {
		return "", fmt.Errorf("pg_dump major must match Postgres server %d; install postgresql-client-%d in the gateway", major, major)
	}
	return command, nil
}

func postgresClientOutput(ctx context.Context, command string, args, environment []string) (string, error) {
	var stdout, stderr limitedBuffer
	stdout.Limit = 4096
	stderr.Limit = maxRestoreLog
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = environment
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", postgresCommandError(command, err, stderr.String())
	}
	return stdout.String(), nil
}
