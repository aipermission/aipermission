package postgresconnector

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

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
	content, cleanupContent, controlsTransaction, err := validatedPostgresRestoreContent(ctx, request)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	defer cleanupContent()
	invocation, err := postgresCLIConnection(ctx, runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	defer invocation.Cleanup()
	completionMarker, err := postgresRestoreCompletionMarker()
	if err != nil {
		return connectors.ActionResult{}, err
	}
	args := invocation.Args
	args = append(args,
		"--no-psqlrc",
		"--single-transaction",
		"--set", "ON_ERROR_STOP=on",
	)
	var stdout limitedBuffer
	stdout.Limit = maxRestoreLog
	var stderr limitedBuffer
	stderr.Limit = maxRestoreLog
	cmd := exec.CommandContext(ctx, "psql", args...)
	cmd.Env = invocation.Env
	cmd.Stdin = io.MultiReader(
		content,
		strings.NewReader("\n\\echo "+completionMarker+"\n"),
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return connectors.ActionResult{}, postgresCommandError("start psql", err, stderr.String())
	}
	if err := cmd.Wait(); err != nil {
		return connectors.ActionResult{}, postgresRestoreError(err, stderr.String())
	}
	if !postgresRestoreCompleted(stdout.String(), completionMarker) {
		return connectors.ActionResult{}, postgresRestoreOutcomeUnknown(
			fmt.Errorf("psql exited before the complete restore stream was acknowledged"),
		)
	}
	output := map[string]any{
		"filename": strings.TrimSpace(request.Filename),
		"stdout":   removePostgresRestoreMarker(stdout.String(), completionMarker),
		"stderr":   stderr.String(),
	}
	if controlsTransaction {
		return connectors.ActionResult{
			Status:      connectors.ResultOutcomeUnknown,
			Output:      output,
			DisplayText: "Postgres SQL restore finished, but transaction control in the artifact prevents atomic completion verification",
			Metadata: map[string]any{
				"connector_kind": Kind,
				"database":       targetString(runtime.Target.Config, "database"),
				"filename":       strings.TrimSpace(request.Filename),
				"reason":         "restore_artifact_controls_transaction",
			},
		}, nil
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

func validatedPostgresRestoreContent(ctx context.Context, request connectors.RestoreRequest) (io.ReadSeeker, func(), bool, error) {
	if request.Content == nil || request.Size <= 0 {
		return nil, nil, false, fmt.Errorf("restore SQL file is empty")
	}
	if request.Size > maxBackupBytes {
		return nil, nil, false, fmt.Errorf("restore SQL file is too large; maximum restore size is 256 MiB")
	}
	content, cleanup, err := seekablePostgresRestoreContent(ctx, request.Content, request.Size)
	if err != nil {
		return nil, nil, false, err
	}
	fail := func(err error) (io.ReadSeeker, func(), bool, error) {
		cleanup()
		return nil, nil, false, err
	}
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return fail(fmt.Errorf("rewind restore SQL file: %w", err))
	}
	read, controlsTransaction, err := validatePostgresRestoreMetaCommands(ctx, content)
	if err != nil {
		return fail(err)
	}
	if read != request.Size {
		return fail(fmt.Errorf("restore SQL file size does not match the uploaded artifact"))
	}
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return fail(fmt.Errorf("rewind restore SQL file: %w", err))
	}
	return content, cleanup, controlsTransaction, nil
}

func seekablePostgresRestoreContent(ctx context.Context, content io.Reader, declaredSize int64) (io.ReadSeeker, func(), error) {
	if seekable, ok := content.(io.ReadSeeker); ok {
		return seekable, func() {}, nil
	}
	temporary, err := os.CreateTemp("", "aipermission-postgres-restore-*")
	if err != nil {
		return nil, nil, fmt.Errorf("stage restore SQL file: %w", err)
	}
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
	}
	written, err := io.Copy(temporary, io.LimitReader(connectors.ReaderWithContext(ctx, content), maxBackupBytes+1))
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("read restore SQL file: %w", err)
	}
	if written != declaredSize {
		cleanup()
		return nil, nil, fmt.Errorf("restore SQL file size does not match the uploaded artifact")
	}
	return temporary, cleanup, nil
}

func validatePostgresRestoreMetaCommands(ctx context.Context, content io.Reader) (int64, bool, error) {
	inCopyData := false
	restrictToken := ""
	restrictSeen := false
	lexical := postgresRestoreLexicalState{statementStart: true}
	reader := bufio.NewReader(content)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, false, err
		}
		rawLine, readErr := reader.ReadString('\n')
		total += int64(len(rawLine))
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\n"))
		line = strings.TrimSuffix(line, "\r")
		if inCopyData {
			if line == `\.` {
				inCopyData = false
			}
		} else if lexical.empty() && strings.HasPrefix(line, `\`) {
			var metaErr error
			restrictToken, restrictSeen, metaErr = validatePostgresRestoreRestriction(line, restrictToken, restrictSeen)
			if metaErr != nil {
				return total, false, metaErr
			}
		} else {
			upper := strings.ToUpper(line)
			if err := lexical.scanLine(line); err != nil {
				return total, false, err
			}
			if lexical.empty() && strings.HasPrefix(upper, "COPY ") && strings.HasSuffix(upper, " FROM STDIN;") {
				inCopyData = true
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return total, false, fmt.Errorf("read restore SQL file: %w", readErr)
			}
			break
		}
	}
	if inCopyData {
		return total, false, fmt.Errorf("restore SQL file contains an unterminated COPY data block")
	}
	if restrictToken != "" {
		return total, false, fmt.Errorf("restore SQL file contains an unmatched psql restriction marker")
	}
	if !lexical.empty() {
		return total, false, fmt.Errorf("restore SQL file contains an unterminated quoted or commented section")
	}
	return total, lexical.controlsTransaction, nil
}

var postgresRestrictionTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func validatePostgresRestoreRestriction(line, activeToken string, seen bool) (string, bool, error) {
	fields := strings.Split(line, " ")
	if len(fields) != 2 || fields[1] == "" || !postgresRestrictionTokenPattern.MatchString(fields[1]) {
		return activeToken, seen, fmt.Errorf("restore SQL file contains an unsafe psql restriction marker")
	}
	switch fields[0] {
	case `\restrict`:
		if seen || activeToken != "" {
			return activeToken, seen, fmt.Errorf("restore SQL file contains multiple psql restriction markers")
		}
		return fields[1], true, nil
	case `\unrestrict`:
		if activeToken == "" || fields[1] != activeToken {
			return activeToken, seen, fmt.Errorf("restore SQL file contains a mismatched psql restriction marker")
		}
		return "", seen, nil
	default:
		return activeToken, seen, fmt.Errorf("restore SQL file contains unsupported psql meta-command %q", fields[0])
	}
}

type postgresRestoreLexicalState struct {
	inSingleQuote       bool
	singleEscapes       bool
	inDoubleQuote       bool
	blockCommentDepth   int
	dollarQuote         string
	statementStart      bool
	secondTokenExpected bool
	firstKeyword        string
	firstToken          strings.Builder
	controlsTransaction bool
}

func (state *postgresRestoreLexicalState) empty() bool {
	return !state.inSingleQuote && !state.inDoubleQuote && state.blockCommentDepth == 0 && state.dollarQuote == ""
}

func (state *postgresRestoreLexicalState) scanLine(line string) error {
	for index := 0; index < len(line); {
		if state.dollarQuote != "" {
			if strings.HasPrefix(line[index:], state.dollarQuote) {
				index += len(state.dollarQuote)
				state.dollarQuote = ""
			} else {
				index++
			}
			continue
		}
		if state.blockCommentDepth > 0 {
			switch {
			case strings.HasPrefix(line[index:], "/*"):
				state.blockCommentDepth++
				index += 2
			case strings.HasPrefix(line[index:], "*/"):
				state.blockCommentDepth--
				index += 2
			default:
				index++
			}
			continue
		}
		if state.inSingleQuote {
			if state.singleEscapes && line[index] == '\\' && index+1 < len(line) {
				index += 2
				continue
			}
			if line[index] == '\'' {
				if index+1 < len(line) && line[index+1] == '\'' {
					index += 2
					continue
				}
				state.inSingleQuote = false
				state.singleEscapes = false
			}
			index++
			continue
		}
		if state.inDoubleQuote {
			if line[index] == '"' {
				if index+1 < len(line) && line[index+1] == '"' {
					index += 2
					continue
				}
				state.inDoubleQuote = false
			}
			index++
			continue
		}
		switch {
		case strings.HasPrefix(line[index:], "--"):
			state.finishFirstToken()
			return nil
		case strings.HasPrefix(line[index:], "/*"):
			state.finishFirstToken()
			state.blockCommentDepth = 1
			index += 2
		case line[index] == '\'':
			state.finishStatementPrefix()
			state.inSingleQuote = true
			state.singleEscapes = postgresEscapeStringPrefix(line, index)
			index++
		case line[index] == '"':
			if postgresUnicodeIdentifierPrefix(line, index) {
				return fmt.Errorf("restore SQL file contains an unsupported Unicode escaped identifier")
			}
			state.finishStatementPrefix()
			state.inDoubleQuote = true
			index++
		case line[index] == '\\':
			return fmt.Errorf("restore SQL file contains an unsupported inline psql meta-command")
		case line[index] == '$':
			state.finishStatementPrefix()
			if delimiter := postgresDollarQuoteDelimiterAt(line, index); delimiter != "" {
				state.dollarQuote = delimiter
				index += len(delimiter)
			} else {
				index++
			}
		case line[index] == ';':
			state.finishFirstToken()
			state.secondTokenExpected = false
			state.firstKeyword = ""
			state.statementStart = true
			index++
		case postgresIdentifierByte(line[index]) && (state.statementStart || state.secondTokenExpected):
			state.firstToken.WriteByte(line[index])
			index++
		case line[index] == ' ' || line[index] == '\t' || line[index] == '\r':
			state.finishFirstToken()
			index++
		default:
			state.finishStatementPrefix()
			index++
		}
	}
	state.finishFirstToken()
	return nil
}

func (state *postgresRestoreLexicalState) finishStatementPrefix() {
	state.finishFirstToken()
	state.statementStart = false
	state.secondTokenExpected = false
	state.firstKeyword = ""
}

func (state *postgresRestoreLexicalState) finishFirstToken() {
	if state.firstToken.Len() == 0 {
		return
	}
	token := strings.ToUpper(state.firstToken.String())
	state.firstToken.Reset()
	state.statementStart = false
	if state.secondTokenExpected {
		if state.firstKeyword == "PREPARE" && token == "TRANSACTION" {
			state.controlsTransaction = true
		}
		state.secondTokenExpected = false
		state.firstKeyword = ""
		return
	}
	switch token {
	case "BEGIN", "START", "COMMIT", "END", "ROLLBACK", "ABORT":
		state.controlsTransaction = true
	case "PREPARE":
		state.firstKeyword = token
		state.secondTokenExpected = true
	}
}

func postgresEscapeStringPrefix(line string, quoteIndex int) bool {
	if quoteIndex == 0 || (line[quoteIndex-1] != 'E' && line[quoteIndex-1] != 'e') {
		return false
	}
	return quoteIndex == 1 || !postgresIdentifierByte(line[quoteIndex-2])
}

func postgresUnicodeIdentifierPrefix(line string, quoteIndex int) bool {
	if quoteIndex < 2 || line[quoteIndex-1] != '&' || (line[quoteIndex-2] != 'U' && line[quoteIndex-2] != 'u') {
		return false
	}
	return quoteIndex == 2 || !postgresIdentifierByte(line[quoteIndex-3])
}

func postgresIdentifierByte(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') ||
		(value >= '0' && value <= '9') || value == '_' || value == '$'
}

func postgresDollarQuoteDelimiterAt(line string, index int) string {
	if index > 0 && postgresIdentifierContinuationByte(line[index-1]) {
		return ""
	}
	return postgresDollarQuoteDelimiter(line[index:])
}

func postgresIdentifierContinuationByte(value byte) bool {
	return postgresIdentifierByte(value) || value >= utf8.RuneSelf
}

func postgresDollarQuoteDelimiter(value string) string {
	if len(value) < 2 || value[0] != '$' {
		return ""
	}
	for index := 1; index < len(value); index++ {
		if value[index] == '$' {
			return value[:index+1]
		}
		if index == 1 && !((value[index] >= 'a' && value[index] <= 'z') ||
			(value[index] >= 'A' && value[index] <= 'Z') || value[index] == '_') {
			return ""
		}
		if index > 1 && !postgresIdentifierByte(value[index]) {
			return ""
		}
	}
	return ""
}

func postgresRestoreError(err error, stderr string) error {
	commandErr := postgresCommandError("psql", err, stderr)
	return postgresRestoreOutcomeUnknown(commandErr)
}

func postgresRestoreOutcomeUnknown(commandErr error) error {
	return connectors.ClassifyOutcomeUnknown("process_observation", map[string]any{
		"recovery_hint": "Inspect the target database before retrying this restore.",
	}, fmt.Errorf("Postgres restore may have committed before the client lost confirmation: %w", commandErr))
}

func postgresRestoreCompletionMarker() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("create Postgres restore completion marker: %w", err)
	}
	return "aipermission_restore_complete_" + hex.EncodeToString(value), nil
}

func postgresRestoreCompleted(output, marker string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSuffix(line, "\r") == marker {
			return true
		}
	}
	return false
}

func removePostgresRestoreMarker(output, marker string) string {
	lines := strings.Split(output, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.TrimSuffix(line, "\r") != marker {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
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
	args := []string{"--dbname", postgresCLIConnectionURL(host, port, username, database), "--no-password"}
	return postgresCLIInvocation{Env: env, Args: args, Cleanup: cleanup}, nil
}

func postgresCLIConnectionURL(host string, port int, username, database string) string {
	connectionURL := url.URL{
		Scheme:  "postgresql",
		User:    url.User(username),
		Host:    net.JoinHostPort(host, strconv.Itoa(port)),
		Path:    "/" + database,
		RawPath: "/" + url.PathEscape(database),
	}
	return connectionURL.String()
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
