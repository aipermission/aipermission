package console

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxManualCommandBufferBytes  = 8192
	maxManualCommandPreviewBytes = 2000
	maxManualCapturedOutputBytes = 1 << 20
	manualCommandReason          = "manual console command not tracked"
	manualTrackedCommandReason   = "manual console command"
	manualCaptureSuperseded      = "manual_capture_superseded"
	manualPromptNotDetected      = "prompt_not_detected"
	manualSessionClosed          = "session_closed"
	manualActiveExecPaused       = "active_exec_paused"
)

type manualInputCapture struct {
	line              string
	initialized       bool
	trusted           bool
	escapePending     bool
	escapeIntro       bool
	escapeOSC         bool
	lastWasCR         bool
	truncated         bool
	heredocTerminator string
	historyRecall     bool
}

func (s *managedConsoleSession) prepareManualInput(data string) []manualCommandRecord {
	if data == "" || s == nil || s.manager == nil || s.manager.db == nil {
		return nil
	}
	if s.activeCommand() != nil {
		s.mu.Lock()
		s.manualInput.reset()
		s.mu.Unlock()
		return nil
	}

	commands := []manualCommandRecord{}
	var completion *manualOutputCompletion
	var activeUpdate *manualActiveCommandUpdate
	s.mu.Lock()
	s.clearManualPauseIfPromptReturnedLocked()
	if s.manualPause != nil {
		s.manualInput.reset()
		s.mu.Unlock()
		return nil
	}
	if strings.ContainsAny(data, "\r\n") && s.manualActive != nil {
		completion = s.manualOutputCompletionLocked()
	}
	if activeUpdate == nil {
		startOffset := len(s.rawTranscript)
		resumePrompt := lastManualShellPrompt(s.rawTranscript)
		for _, command := range s.manualInput.consume(data) {
			if command.Command != "" {
				command.StartOffset = startOffset
				command.ResumePrompt = resumePrompt
				commands = append(commands, command)
			}
		}
		commands = collapseManualCommandRecords(commands)
		if completion == nil && s.manualActive != nil && len(commands) > 0 {
			if manualActiveIsHistoryRecall(s.manualActive) {
				active := *s.manualActive
				completion = s.downgradeManualOutputCaptureLocked("history_recall_untracked", false)
				s.pauseManualCaptureAfterActiveLocked(active)
				s.manualInput.reset()
			} else {
				activeUpdate = s.appendManualActiveCommandsLocked(commands)
			}
			commands = nil
		}
	}
	s.mu.Unlock()
	if completion != nil {
		s.finishManualOutputCapture(completion)
	}
	if activeUpdate != nil {
		s.updateManualActiveCommand(activeUpdate)
	}
	return commands
}

func (s *managedConsoleSession) persistManualInput(commands []manualCommandRecord) {
	for _, command := range commands {
		if err := s.insertManualCommand(command); err != nil {
			logConsolePersistError("manual_history", s.id, err)
		}
	}
}

func (s *managedConsoleSession) recordManualInput(data string) {
	s.persistManualInput(s.prepareManualInput(data))
}

func (c *manualInputCapture) consume(data string) []manualCommandRecord {
	data = stripBracketedPasteMarkers(data)
	if !c.initialized {
		c.initialized = true
		c.trusted = true
	}
	records := []manualCommandRecord{}
	for _, r := range data {
		if c.consumeEscape(r) {
			continue
		}
		switch r {
		case '\x1b':
			c.trusted = false
			c.escapePending = true
			c.escapeIntro = true
			c.escapeOSC = false
		case '\r':
			records = append(records, c.finishLine()...)
			c.lastWasCR = true
		case '\n':
			if c.lastWasCR {
				c.lastWasCR = false
				continue
			}
			records = append(records, c.finishLine()...)
		case '\x03', '\x04':
			c.reset()
		case '\b', '\x7f':
			c.line = trimLastRune(c.line)
			c.lastWasCR = false
		case '\t':
			c.appendRune(r)
			c.lastWasCR = false
		default:
			c.lastWasCR = false
			if r < 0x20 || r == 0x7f {
				c.trusted = false
				continue
			}
			c.appendRune(r)
		}
	}
	return records
}

func (c *manualInputCapture) consumeEscape(r rune) bool {
	if !c.escapePending {
		return false
	}
	if c.escapeIntro {
		c.escapeIntro = false
		if r == ']' {
			c.escapeOSC = true
		}
		if r != '[' && r != ']' {
			c.escapePending = false
		}
		return true
	}
	if c.escapeOSC {
		if r == '\a' {
			c.escapePending = false
			c.escapeOSC = false
		}
		return true
	}
	if r >= '@' && r <= '~' {
		if r == 'A' || r == 'B' {
			c.historyRecall = true
		}
		c.escapePending = false
	}
	return true
}

func (c *manualInputCapture) appendRune(r rune) {
	if len(c.line) >= maxManualCommandBufferBytes {
		c.truncated = true
		return
	}
	c.line += string(r)
	if len(c.line) > maxManualCommandBufferBytes {
		c.line = c.line[:maxManualCommandBufferBytes]
		for len(c.line) > 0 && !utf8.ValidString(c.line) {
			c.line = c.line[:len(c.line)-1]
		}
		c.truncated = true
	}
}

func (c *manualInputCapture) finishLine() []manualCommandRecord {
	line := c.line
	trusted := c.trusted
	truncated := c.truncated
	c.line = ""
	c.initialized = true
	c.trusted = true
	c.truncated = false
	c.escapePending = false
	c.escapeIntro = false
	c.escapeOSC = false
	historyRecall := c.historyRecall
	c.historyRecall = false

	if c.heredocTerminator != "" {
		if strings.TrimSpace(line) == c.heredocTerminator {
			c.heredocTerminator = ""
		}
		return nil
	}

	command := strings.TrimSpace(line)
	if command == "" {
		if historyRecall {
			return []manualCommandRecord{{
				Command:                  "command recalled with arrow key",
				TrackingReason:           "history_recall_untracked",
				TrackOutput:              true,
				CompletionTrackingReason: "history_recall_untracked",
			}}
		}
		return nil
	}
	if !trusted {
		return []manualCommandRecord{{
			Command:        manualCommandPreview(command, len(command) > maxManualCommandPreviewBytes),
			TrackingReason: "untrusted_command_text",
		}}
	}

	record := classifyManualCommand(command, truncated)
	if terminator := heredocTerminator(command); terminator != "" {
		c.heredocTerminator = terminator
	}
	return []manualCommandRecord{record}
}

func stripBracketedPasteMarkers(data string) string {
	if !strings.Contains(data, "\x1b[") {
		return data
	}
	data = strings.ReplaceAll(data, "\x1b[200~", "")
	data = strings.ReplaceAll(data, "\x1b[201~", "")
	return data
}

func (c *manualInputCapture) reset() {
	c.line = ""
	c.initialized = true
	c.trusted = true
	c.escapePending = false
	c.escapeIntro = false
	c.escapeOSC = false
	c.lastWasCR = false
	c.truncated = false
	c.heredocTerminator = ""
	c.historyRecall = false
}

type manualCommandRecord struct {
	Command                  string
	TrackingReason           string
	TrackOutput              bool
	StartOffset              int
	ResumePrompt             string
	CompletionTrackingReason string
}

func collapseManualCommandRecords(commands []manualCommandRecord) []manualCommandRecord {
	if len(commands) <= 1 {
		return commands
	}
	trackOutput := true
	startOffset := commands[0].StartOffset
	parts := make([]string, 0, len(commands))
	for _, command := range commands {
		parts = append(parts, command.Command)
		if !command.TrackOutput {
			trackOutput = false
		}
	}
	joined := strings.Join(parts, "\n")
	reason := "manual_output_not_tracked"
	if !trackOutput {
		reason = "compound_command"
	}
	if len(joined) > maxManualCommandPreviewBytes {
		reason = "command_preview_truncated"
		trackOutput = false
	}
	return []manualCommandRecord{{
		Command:                  manualCommandPreview(joined, reason == "command_preview_truncated"),
		TrackingReason:           reason,
		TrackOutput:              trackOutput,
		StartOffset:              startOffset,
		ResumePrompt:             commands[0].ResumePrompt,
		CompletionTrackingReason: commands[0].CompletionTrackingReason,
	}}
}

type manualOutputCompletion struct {
	RequestID       int64
	Status          string
	Stdout          string
	OutputTruncated bool
	TrackingReason  string
	Error           string
}

type manualActiveCommandUpdate struct {
	RequestID      int64
	Command        string
	TrackingReason string
	Downgrade      bool
}

func (s *managedConsoleSession) appendManualActiveCommandsLocked(commands []manualCommandRecord) *manualActiveCommandUpdate {
	if s.manualActive == nil || len(commands) == 0 {
		return nil
	}
	active := *s.manualActive
	parts := []string{strings.TrimSpace(active.Command)}
	trackOutput := true
	reason := "manual_output_not_tracked"
	for _, command := range commands {
		if command.Command == "" {
			continue
		}
		parts = append(parts, command.Command)
		if !command.TrackOutput {
			trackOutput = false
			reason = "compound_command"
		}
	}
	combined := strings.TrimSpace(strings.Join(parts, "\n"))
	if combined == "" {
		return nil
	}
	if len(combined) > maxManualCommandPreviewBytes {
		combined = manualCommandPreview(combined, true)
		trackOutput = false
		reason = "command_preview_truncated"
	}
	active.Command = combined
	if trackOutput {
		s.manualActive = &active
	} else {
		s.manualActive = nil
	}
	return &manualActiveCommandUpdate{
		RequestID:      active.RequestID,
		Command:        combined,
		TrackingReason: reason,
		Downgrade:      !trackOutput,
	}
}

func classifyManualCommand(command string, truncated bool) manualCommandRecord {
	reason := "manual_output_not_tracked"
	if truncated || len(command) > maxManualCommandPreviewBytes {
		reason = "command_preview_truncated"
	}
	if heredocTerminator(command) != "" {
		reason = "multiline_or_heredoc"
	}
	if reason == "manual_output_not_tracked" {
		reason = classifyManualCommandReason(command)
	}
	return manualCommandRecord{
		Command:        manualCommandPreview(command, reason == "command_preview_truncated" || reason == "multiline_or_heredoc"),
		TrackingReason: reason,
		TrackOutput:    reason == "manual_output_not_tracked",
	}
}

func classifyManualCommandReason(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "manual_output_not_tracked"
	}
	base := fields[0]
	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}
	switch base {
	case "nano", "vi", "vim", "nvim", "emacs":
		return "interactive_editor"
	case "psql", "mysql", "redis-cli", "sqlite3", "python", "python3", "node", "irb", "rails", "php":
		if len(fields) == 1 || strings.HasPrefix(command, base+" -i") {
			return "interactive_repl"
		}
	case "top", "htop", "btop", "less", "more", "man", "watch":
		return "interactive_tui"
	case "ssh", "sftp", "ftp", "telnet", "mosh", "tmux", "screen":
		return "nested_shell"
	case "tail":
		if containsField(fields[1:], "-f") || containsField(fields[1:], "--follow") {
			return "long_running_stream"
		}
	case "docker", "kubectl":
		if commandContainsInteractiveFlag(fields[1:]) {
			return "nested_shell"
		}
	case "sudo":
		return "may_prompt"
	}
	if strings.HasSuffix(strings.TrimSpace(command), "&") {
		return "background_job"
	}
	return "manual_output_not_tracked"
}

func containsField(fields []string, value string) bool {
	for _, field := range fields {
		if field == value {
			return true
		}
	}
	return false
}

func commandContainsInteractiveFlag(fields []string) bool {
	for _, field := range fields {
		if field == "-it" || field == "-ti" || field == "-i" || field == "-t" || field == "--tty" || field == "--stdin" {
			return true
		}
	}
	return false
}

func manualCommandPreview(command string, incomplete bool) string {
	command = strings.TrimSpace(command)
	limit := maxManualCommandPreviewBytes
	if incomplete && limit > 4 {
		limit -= 4
	}
	if len(command) > limit {
		command = command[:limit]
		for len(command) > 0 && !utf8.ValidString(command) {
			command = command[:len(command)-1]
		}
		incomplete = true
	}
	if incomplete && !strings.HasSuffix(command, "...") {
		command = strings.TrimRight(command, " \t") + " ..."
	}
	return command
}

func heredocTerminator(command string) string {
	index := strings.Index(command, "<<")
	if index < 0 {
		return ""
	}
	rest := strings.TrimSpace(command[index+2:])
	if strings.HasPrefix(rest, "-") {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "-"))
	}
	if rest == "" {
		return ""
	}
	token := strings.Fields(rest)[0]
	token = strings.Trim(token, `"'`)
	if token == "" || strings.ContainsAny(token, `/\`) {
		return ""
	}
	return token
}

func trimLastRune(value string) string {
	if value == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(value)
	if size <= 0 {
		return ""
	}
	return value[:len(value)-size]
}

func (s *managedConsoleSession) insertManualCommand(command manualCommandRecord) error {
	if command.Command == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	storedCommand := s.manager.redactText(command.Command)
	storedReason := s.manager.redactText(manualCommandReason)
	status := "untracked"
	completedAt := sql.NullString{String: now, Valid: true}
	if command.TrackOutput {
		storedReason = s.manager.redactText(manualTrackedCommandReason)
		status = "running"
		completedAt = sql.NullString{}
		s.closeStaleManualRunningRows(0, manualCaptureSuperseded)
	}
	trackingReason := s.manager.redactText(command.TrackingReason)
	result, err := s.manager.db.ExecContext(context.Background(), `
		INSERT INTO command_requests (server_id, source, command, encrypted_command, reason, status, tracking_reason, stdout, stderr, session_id, created_at, completed_at)
		VALUES (?, 'manual', ?, '', ?, ?, ?, '', '', ?, ?, ?)`,
		s.serverID,
		storedCommand,
		storedReason,
		status,
		trackingReason,
		s.id,
		now,
		completedAt,
	)
	if err != nil {
		return fmt.Errorf("insert manual command history: %w", err)
	}
	if command.TrackOutput {
		requestID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read manual command history id: %w", err)
		}
		completion := s.setManualOutputCapture(consoleSessionManualCapture{
			RequestID:                requestID,
			Command:                  command.Command,
			StartOffset:              command.StartOffset,
			ResumePrompt:             command.ResumePrompt,
			Started:                  time.Now(),
			CompletionTrackingReason: command.CompletionTrackingReason,
		})
		if completion != nil {
			go s.finishManualOutputCapture(completion)
		}
	} else {
		s.pauseManualCaptureAfterCommand(command)
	}
	return nil
}

func (s *managedConsoleSession) pauseManualCaptureAfterCommand(command manualCommandRecord) {
	if !manualReasonPausesCapture(command.TrackingReason) {
		return
	}
	s.mu.Lock()
	s.manualPause = &consoleSessionManualPause{
		Prompt:      command.ResumePrompt,
		Reason:      command.TrackingReason,
		StartOffset: command.StartOffset,
	}
	s.manualInput.reset()
	s.mu.Unlock()
}

func (s *managedConsoleSession) pauseManualCaptureAfterActiveLocked(active consoleSessionManualCapture) {
	s.manualPause = &consoleSessionManualPause{
		Prompt:      active.ResumePrompt,
		Reason:      active.CompletionTrackingReason,
		StartOffset: active.StartOffset,
	}
}

func (s *managedConsoleSession) setManualOutputCapture(capture consoleSessionManualCapture) *manualOutputCompletion {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manualActive = &capture
	return s.manualOutputCompletionLocked()
}

func (s *managedConsoleSession) updateManualActiveCommand(update *manualActiveCommandUpdate) {
	if update == nil || s == nil || s.manager == nil || s.manager.db == nil {
		return
	}
	command := s.manager.redactText(update.Command)
	trackingReason := s.manager.redactText(update.TrackingReason)
	if update.Downgrade {
		now := time.Now().UTC().Format(time.RFC3339)
		_, err := s.manager.db.ExecContext(context.Background(), `
			UPDATE command_requests
			SET command = ?, status = 'untracked', tracking_reason = ?, completed_at = COALESCE(completed_at, ?)
			WHERE id = ? AND status = 'running'`,
			command,
			trackingReason,
			now,
			update.RequestID,
		)
		if err != nil {
			logConsolePersistError("manual_history_update", s.id, err)
		}
		s.closeStaleManualRunningRows(update.RequestID, update.TrackingReason)
		return
	}
	_, err := s.manager.db.ExecContext(context.Background(), `
		UPDATE command_requests
		SET command = ?
		WHERE id = ? AND status = 'running'`,
		command,
		update.RequestID,
	)
	if err != nil {
		logConsolePersistError("manual_history_update", s.id, err)
	}
}

func (s *managedConsoleSession) manualOutputCompletionLocked() *manualOutputCompletion {
	if s.manualActive == nil {
		return nil
	}
	active := *s.manualActive
	startOffset := active.StartOffset
	truncated := false
	if startOffset > len(s.rawTranscript) {
		startOffset = 0
		truncated = true
	}
	segment := s.rawTranscript[startOffset:]
	if !manualSegmentHasPrompt(segment) {
		return nil
	}
	stdout, outputTruncated := manualCapturedOutput(segment, active.Command)
	truncated = truncated || outputTruncated
	status := "completed"
	errorText := ""
	if strings.Contains(segment, "^C") {
		status = "canceled"
		errorText = "manual command interrupted"
	}
	trackingReason := active.CompletionTrackingReason
	if strings.TrimSpace(trackingReason) == "" {
		trackingReason = "exit_code_unavailable"
	}
	s.manualActive = nil
	return &manualOutputCompletion{
		RequestID:       active.RequestID,
		Status:          status,
		Stdout:          stdout,
		OutputTruncated: truncated,
		TrackingReason:  trackingReason,
		Error:           errorText,
	}
}

func (s *managedConsoleSession) manualActiveHasOutputLocked() bool {
	if s.manualActive == nil {
		return false
	}
	active := *s.manualActive
	startOffset := active.StartOffset
	if startOffset > len(s.rawTranscript) {
		startOffset = 0
	}
	stdout, _ := manualCapturedOutput(s.rawTranscript[startOffset:], active.Command)
	return strings.TrimSpace(PlainOutput(stdout)) != ""
}

func (s *managedConsoleSession) downgradeManualOutputCaptureLocked(reason string, captureOutput bool) *manualOutputCompletion {
	if s.manualActive == nil {
		return nil
	}
	active := *s.manualActive
	startOffset := active.StartOffset
	truncated := false
	if startOffset > len(s.rawTranscript) {
		startOffset = 0
		truncated = true
	}
	stdout := ""
	outputTruncated := false
	if captureOutput {
		stdout, outputTruncated = manualCapturedOutput(s.rawTranscript[startOffset:], active.Command)
	}
	s.manualActive = nil
	return &manualOutputCompletion{
		RequestID:       active.RequestID,
		Status:          "untracked",
		Stdout:          stdout,
		OutputTruncated: truncated || outputTruncated,
		TrackingReason:  reason,
	}
}

func (s *managedConsoleSession) finishManualOutputCapture(completion *manualOutputCompletion) {
	if completion == nil || s == nil || s.manager == nil || s.manager.db == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	stdout := s.manager.redactText(PlainOutput(completion.Stdout))
	errorText := s.manager.redactText(completion.Error)
	trackingReason := s.manager.redactText(completion.TrackingReason)
	outputTruncated := 0
	if completion.OutputTruncated {
		outputTruncated = 1
	}
	_, err := s.manager.db.ExecContext(context.Background(), `
		UPDATE command_requests
		SET status = ?, stdout = ?, stderr = '', tracking_reason = ?, output_truncated = ?, error = ?, completed_at = ?
		WHERE id = ? AND source = 'manual'`,
		completion.Status,
		stdout,
		trackingReason,
		outputTruncated,
		errorText,
		now,
		completion.RequestID,
	)
	if err != nil {
		logConsolePersistError("manual_history_finish", s.id, err)
	}
	s.closeStaleManualRunningRows(completion.RequestID, manualCaptureSuperseded)
}

func (s *managedConsoleSession) closeManualOutputCapture(reason string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.manualPause = nil
	completion := s.manualOutputCompletionLocked()
	if completion == nil {
		completion = s.downgradeManualOutputCaptureLocked(reason, true)
	}
	s.mu.Unlock()
	s.finishManualOutputCapture(completion)
}

func (s *managedConsoleSession) closeStaleManualRunningRows(exceptID int64, reason string) {
	if s == nil || s.manager == nil || s.manager.db == nil || s.id < 1 {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = manualCaptureSuperseded
	}
	now := time.Now().UTC().Format(time.RFC3339)
	query := `
		UPDATE command_requests
		SET status = 'untracked', tracking_reason = ?, completed_at = COALESCE(completed_at, ?)
		WHERE source = 'manual'
			AND session_id = ?
			AND status = 'running'
			AND (? = 0 OR id <> ?)`
	if _, err := s.manager.db.ExecContext(context.Background(), query, s.manager.redactText(reason), now, s.id, exceptID, exceptID); err != nil {
		logConsolePersistError("manual_history_stale", s.id, err)
	}
}

func (s *managedConsoleSession) clearManualPauseIfPromptReturnedLocked() {
	if s == nil || s.manualPause == nil {
		return
	}
	startOffset := s.manualPause.StartOffset
	if startOffset < 0 || startOffset > len(s.rawTranscript) {
		startOffset = 0
	}
	if startOffset < len(s.rawTranscript) && manualTranscriptEndsWithPrompt(s.rawTranscript[startOffset:], s.manualPause.Prompt) {
		s.manualPause = nil
		s.manualInput.reset()
	}
}

func manualReasonPausesCapture(reason string) bool {
	switch reason {
	case "interactive_editor", "interactive_repl", "interactive_tui", "nested_shell", "long_running_stream", "may_prompt":
		return true
	default:
		return false
	}
}

func manualActiveIsHistoryRecall(active *consoleSessionManualCapture) bool {
	if active == nil {
		return false
	}
	return active.CompletionTrackingReason == "history_recall_untracked" || active.Command == "command recalled with arrow key"
}

func manualSegmentHasPrompt(segment string) bool {
	plain := ansiSequencePattern.ReplaceAllString(segment, "")
	plain = strings.ReplaceAll(plain, "\r\n", "\n")
	plain = strings.ReplaceAll(plain, "\r", "\n")
	plain = strings.TrimRight(plain, " \t\n")
	if plain == "" {
		return false
	}
	lines := strings.Split(plain, "\n")
	last := strings.TrimRight(lines[len(lines)-1], " \t")
	return bareShellPromptPattern.MatchString(last)
}

func lastManualShellPrompt(transcript string) string {
	plain := normalizedPlainTerminalText(transcript)
	plain = strings.TrimRight(plain, " \t\n")
	if plain == "" {
		return ""
	}
	lines := strings.Split(plain, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if prompt := manualPromptPrefix(lines[index]); prompt != "" {
			return prompt
		}
	}
	return ""
}

func manualTranscriptEndsWithPrompt(transcript string, prompt string) bool {
	prompt = strings.TrimRight(normalizedPlainTerminalText(prompt), " \t\n")
	plain := strings.TrimRight(normalizedPlainTerminalText(transcript), " \t\n")
	if plain == "" {
		return false
	}
	lines := strings.Split(plain, "\n")
	last := strings.TrimRight(lines[len(lines)-1], " \t")
	if !bareShellPromptPattern.MatchString(last) {
		return false
	}
	return prompt == "" || last == prompt
}

func manualPromptPrefix(line string) string {
	line = strings.TrimRight(normalizedPlainTerminalText(line), " \t\n")
	if line == "" {
		return ""
	}
	if bareShellPromptPattern.MatchString(line) {
		return line
	}
	if !shellPromptPattern.MatchString(line) {
		return ""
	}
	hashIndex := strings.LastIndex(line, "# ")
	dollarIndex := strings.LastIndex(line, "$ ")
	index := hashIndex
	if dollarIndex > index {
		index = dollarIndex
	}
	if index < 0 {
		return ""
	}
	return strings.TrimRight(line[:index+1], " \t")
}

func manualCapturedOutput(segment string, command string) (string, bool) {
	plain := normalizedPlainTerminalText(segment)
	lines := strings.Split(plain, "\n")
	commandLines := manualCommandLines(command)
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		if lineContainsAnyCommandEcho(line, commandLines) {
			continue
		}
		cleaned = append(cleaned, line)
	}
	lines = cleaned
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 0 && bareShellPromptPattern.MatchString(strings.TrimRight(lines[len(lines)-1], " \t")) {
		lines = lines[:len(lines)-1]
	}
	output := strings.Join(lines, "\n")
	truncated := false
	if len(output) > maxManualCapturedOutputBytes {
		output = TailStringByBytes(output, maxManualCapturedOutputBytes)
		truncated = true
	}
	return output, truncated
}

func normalizedPlainTerminalText(value string) string {
	value = ansiSequencePattern.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return value
}

func manualCommandLines(command string) []string {
	lines := strings.Split(strings.ReplaceAll(command, "\r\n", "\n"), "\n")
	values := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			values = append(values, line)
		}
	}
	return values
}

func lineContainsAnyCommandEcho(line string, commands []string) bool {
	for _, command := range commands {
		if lineContainsCommandEcho(line, command) {
			return true
		}
	}
	return false
}

func lineContainsCommandEcho(line string, command string) bool {
	line = strings.TrimSpace(line)
	command = strings.TrimSpace(command)
	if line == command {
		return true
	}
	if bareShellPromptPattern.MatchString(line) || !shellPromptPattern.MatchString(line) {
		return false
	}
	if index := strings.LastIndex(line, "# "); index >= 0 {
		return strings.TrimSpace(line[index+2:]) == command
	}
	if index := strings.LastIndex(line, "$ "); index >= 0 {
		return strings.TrimSpace(line[index+2:]) == command
	}
	return false
}
