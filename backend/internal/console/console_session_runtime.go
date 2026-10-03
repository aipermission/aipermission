package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	consolepersistence "github.com/aipermission/aipermission/backend/internal/console/persistence"
	"github.com/aipermission/aipermission/backend/internal/console/terminaltext"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
	"github.com/aipermission/aipermission/backend/internal/timeformat"
	"github.com/gorilla/websocket"
)

func (s *managedConsoleSession) run() {
	defer func() {
		s.destroySensitiveRuntime()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := s.finalize(ctx); err != nil {
			logConsolePersistError("finalize", s.id, err)
		}
		cancel()
		if s.done != nil {
			close(s.done)
		}
	}()

	if s.manager.openRuntime == nil {
		s.failStart(fmt.Errorf("console transport is not configured"))
		return
	}
	runtime, err := s.manager.openRuntime(s.ctx, RuntimeOpenRequest{
		RuntimeID:      s.runtimeID,
		Generation:     s.generation,
		Rows:           s.rows,
		Cols:           s.cols,
		Params:         maps.Clone(s.params),
		HasEnvironment: s.environment != nil || s.prepareEnvironment != nil,
	})
	if err != nil {
		s.failStart(err)
		return
	}
	if runtime == nil {
		s.failStart(fmt.Errorf("console transport returned no session"))
		return
	}
	stdin := runtime.Stdin
	if stdin == nil {
		_ = runtime.close()
		s.failStart(fmt.Errorf("console transport did not provide stdin"))
		return
	}
	if !s.publishRuntime(runtime) {
		s.markStarted(ErrSessionClosing)
		return
	}
	defer s.closeRuntime()

	if s.environment != nil || s.prepareEnvironment != nil {
		if err := s.applyEnvironment(runtime); err != nil {
			s.failStart(err)
			return
		}
	}

	if runtime.Output == nil {
		s.failStart(fmt.Errorf("console transport did not provide output"))
		return
	}
	if runtime.Done == nil {
		s.failStart(fmt.Errorf("console transport did not provide completion signal"))
		return
	}

	s.setStatus("connected", "")
	s.broadcast(ptyServerMessage{Type: "ready", Status: "connected", SessionID: s.id})
	s.markStarted(nil)

	if runtime.StartupInputAfterConnect != "" {
		if _, err := io.WriteString(stdin, runtime.StartupInputAfterConnect); err != nil {
			s.fail(fmt.Sprintf("write startup input: %v", err))
			return
		}
	}

	s.consumeRuntime(runtime)
}

func (s *managedConsoleSession) failStart(err error) {
	s.markStarted(err)
	s.fail(err.Error())
}

func (s *managedConsoleSession) destroySensitiveRuntime() {
	if s == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	// Admitted persistence work must retain the exact redactor until it drains.
	s.drainOwnedWork()
	s.closeManualOutputCapture(manualSessionClosed)
	if err := s.closeStaleManualRunningRows(0, manualSessionClosed); err != nil {
		logConsolePersistError("manual_history_final", s.id, err)
	}
	s.closeExactRedactor()
	s.environment.Destroy()
	if s.startupAdmissionRelease != nil {
		s.startupAdmissionRelease()
	}
}

func (s *managedConsoleSession) applyEnvironment(runtime *RuntimeSession) error {
	if runtime == nil || runtime.ApplyEnvironment == nil {
		return fmt.Errorf("console transport does not support session environments")
	}
	preparation := EnvironmentPreparation{Environment: s.environment}
	if s.prepareEnvironment != nil {
		var err error
		preparation, err = s.prepareEnvironment(s.ctx, runtime.PeerIdentity)
		if err != nil {
			return err
		}
	}
	if preparation.Release != nil {
		defer preparation.Release()
	}
	if preparation.Environment == nil || preparation.Environment.Len() == 0 {
		return fmt.Errorf("session environment preparation returned no values")
	}
	redactors := make([]*sessionenv.Redactor, 0, 3)
	for range 3 {
		redactor, err := preparation.Environment.ExactValueRedactor()
		if err != nil {
			for _, item := range redactors {
				_ = item.Close()
			}
			preparation.Environment.Destroy()
			return fmt.Errorf("prepare console redaction: %w", err)
		}
		redactors = append(redactors, redactor)
	}
	s.mu.Lock()
	s.exactRedactor = redactors[0]
	s.stdoutExactRedactor = redactors[1]
	s.stderrExactRedactor = redactors[2]
	s.environment = preparation.Environment
	s.mu.Unlock()
	if err := runtime.ApplyEnvironment(s.ctx, preparation.Environment); err != nil {
		return err
	}
	if preparation.PostValidate != nil {
		if err := preparation.PostValidate(s.ctx); err != nil {
			return err
		}
	}
	if preparation.Finalize != nil {
		if err := preparation.Finalize(s.ctx, s.handle()); err != nil {
			return err
		}
	}
	return nil
}

func (s *managedConsoleSession) publishRuntime(runtime *RuntimeSession) bool {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		_ = runtime.close()
		return false
	}
	s.runtime = runtime
	s.stdin = runtime.Stdin
	s.mu.Unlock()
	return true
}

func (s *managedConsoleSession) consumeRuntime(runtime *RuntimeSession) {
	output := runtime.Output
	done := runtime.Done
	var completion error
	for output != nil || done != nil {
		select {
		case item, ok := <-output:
			if !ok {
				if err := s.flushOutputStreams(); err != nil {
					s.finish("error", "console transcript persistence interrupted; output observation is incomplete")
					return
				}
				output = nil
				continue
			}
			redactor := s.stdoutExactRedactor
			if item.Kind == RuntimeStderr {
				redactor = s.stderrExactRedactor
			}
			if err := s.appendStreamOutput(item.Data, redactor, item.Kind); err != nil {
				s.finish("error", "console transcript persistence interrupted; output observation is incomplete")
				return
			}
		case err, ok := <-done:
			if ok {
				completion = err
			}
			done = nil
		case <-s.ctx.Done():
			_ = s.closeRuntime()
			s.finish("closed", "")
			return
		}
	}
	_ = s.closeRuntime()
	if completion != nil && !errors.Is(completion, io.EOF) {
		s.finish("closed", completion.Error())
		return
	}
	s.finish("closed", "")
}

// addClientWithSnapshot returns with writeMu locked. The caller must send the
// snapshot before unlocking it so later broadcasts cannot overtake it.
// Output recipients are frozen with transcript appends to avoid replaying data
// already included in this snapshot.
func (s *managedConsoleSession) addClientWithSnapshot(ws *websocket.Conn) (*sync.Mutex, string, string, error) {
	writeMu := &sync.Mutex{}
	writeMu.Lock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		writeMu.Unlock()
		return nil, "", "", ErrSessionClosing
	}
	if len(s.clients) >= maxConsoleClientsPerSession {
		writeMu.Unlock()
		return nil, "", "", ErrClientLimit
	}
	s.clients[ws] = writeMu
	return writeMu, s.status, s.transcript, nil
}

func (s *managedConsoleSession) removeClient(ws *websocket.Conn) {
	s.mu.Lock()
	delete(s.clients, ws)
	s.mu.Unlock()
}

func (s *managedConsoleSession) snapshot() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.transcript
}

func (s *managedConsoleSession) dimensions() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

func (s *managedConsoleSession) writeInput(data string) error {
	_, err := s.writeInputAttempt(data)
	return err
}

// A payload write attempt may have reached the peer even when its acknowledgement
// is lost. Readiness rejection, in contrast, never calls the transport writer.
func (s *managedConsoleSession) writeInputAttempt(data string) (bool, error) {
	if data == "" {
		return false, nil
	}
	s.mu.Lock()
	if s.stdin == nil || s.status != "connected" {
		s.mu.Unlock()
		return false, fmt.Errorf("console session is not ready")
	}
	stdin := s.stdin
	s.mu.Unlock()
	written, err := io.WriteString(stdin, data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	return true, err
}

func (s *managedConsoleSession) resize(cols int, rows int) {
	if cols < 1 || rows < 1 {
		return
	}
	s.mu.Lock()
	s.cols = cols
	s.rows = rows
	runtime := s.runtime
	s.mu.Unlock()
	if runtime != nil && runtime.Resize != nil {
		_ = runtime.Resize(cols, rows)
	}
	if _, err := s.manager.db.Exec(`UPDATE console_sessions SET cols = ?, rows = ?, updated_at = ? WHERE id = ?`, cols, rows, timeformat.Now(), s.id); err != nil {
		logConsolePersistError("resize", s.id, err)
	}
}

func (s *managedConsoleSession) close() {
	s.beginClose()
}

func (s *managedConsoleSession) beginClose() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closing = true
	if s.status == "connecting" || s.status == "connected" {
		s.status = "closing"
	}
	clients := make([]*websocket.Conn, 0, len(s.clients))
	for client := range s.clients {
		clients = append(clients, client)
		delete(s.clients, client)
	}
	s.mu.Unlock()
	s.closeWorkAdmission()
	if s.cancel != nil {
		s.cancel()
	}
	s.closeKick.Do(func() { go func() { _ = s.closeRuntime() }() })
	for _, client := range clients {
		_ = client.Close()
	}
}

func (s *managedConsoleSession) runOwnedWork(run func()) bool {
	if run == nil || !s.admitOwnedWork() {
		return false
	}
	go func() {
		defer s.workWG.Done()
		run()
	}()
	return true
}

func (s *managedConsoleSession) admitOwnedWork() bool {
	if s == nil {
		return false
	}
	s.workMu.Lock()
	defer s.workMu.Unlock()
	if s.workClosed {
		return false
	}
	s.workWG.Add(1)
	return true
}

func (s *managedConsoleSession) runAuthorizedWork(run func() error) error {
	if run == nil || !s.admitOwnedWork() {
		return ErrSessionClosing
	}
	defer s.workWG.Done()
	return run()
}

func (s *managedConsoleSession) closeWorkAdmission() {
	s.workMu.Lock()
	s.workClosed = true
	s.workMu.Unlock()
}

func (s *managedConsoleSession) drainOwnedWork() {
	if s == nil {
		return
	}
	s.closeWorkAdmission()
	s.workWG.Wait()
}

func (s *managedConsoleSession) closeRuntime() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	runtime := s.runtime
	s.mu.Unlock()
	if runtime == nil {
		return nil
	}
	s.closeOnce.Do(func() { s.closeErr = runtime.close() })
	return s.closeErr
}

func (session *RuntimeSession) close() error {
	if session == nil || session.Close == nil {
		return nil
	}
	return session.Close()
}

func (s *managedConsoleSession) fail(message string) {
	message = s.redactForPersistence(message)
	s.setStatus("error", message)
	s.broadcast(ptyServerMessage{Type: "error", Status: "error", Data: message, SessionID: s.id})
	s.finish("error", message)
}

func (s *managedConsoleSession) finish(status string, message string) {
	persistedMessage := s.redactForPersistence(message)
	s.closeManualOutputCapture(manualSessionClosed)
	s.mu.Lock()
	s.status = status
	s.finalStatus = status
	s.finalMessage = persistedMessage
	if persistedMessage != "" {
		s.errText = persistedMessage
	}
	s.mu.Unlock()
	s.broadcast(ptyServerMessage{Type: "exit", Status: status, Data: persistedMessage, SessionID: s.id})
}

func (s *managedConsoleSession) markStarted(err error) {
	if err != nil {
		message := s.redactForPersistence(err.Error())
		if message != err.Error() {
			err = &redactedConsoleError{cause: err, message: message}
		}
	}
	s.startOnce.Do(func() {
		s.mu.Lock()
		s.startErr = err
		s.mu.Unlock()
		close(s.start)
	})
}

func (s *managedConsoleSession) waitStart(ctx context.Context) error {
	if s == nil || s.start == nil {
		return nil
	}
	select {
	case <-s.start:
		s.mu.Lock()
		err := s.startErr
		s.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *managedConsoleSession) waitDone(ctx context.Context) error {
	if s == nil || s.done == nil {
		return nil
	}
	select {
	case <-s.done:
		return s.finalize(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *managedConsoleSession) setStatus(status string, message string) {
	now := timeformat.Now()
	persistedMessage := s.redactForPersistence(message)
	s.mu.Lock()
	s.status = status
	s.errText = persistedMessage
	s.mu.Unlock()
	if _, err := s.manager.db.Exec(`UPDATE console_sessions SET status = ?, error = ?, updated_at = ? WHERE id = ?`, status, persistedMessage, now, s.id); err != nil {
		logConsolePersistError("set_status", s.id, err)
	}
}

func (s *managedConsoleSession) appendOutput(data string) {
	s.mu.Lock()
	redactor := s.stdoutExactRedactor
	if redactor == nil {
		redactor = s.exactRedactor
	}
	s.mu.Unlock()
	s.appendStreamOutput(data, redactor, RuntimeStdout)
}

func (s *managedConsoleSession) appendStreamOutput(data string, redactor *sessionenv.Redactor, kind RuntimeOutputKind) error {
	s.mu.Lock()
	redactionClosed := s.exactRedactionClosed
	s.mu.Unlock()
	if redactionClosed {
		return nil
	}
	data = s.outputStreams.Write(int(kind), data, redactor)
	return s.appendSafeOutput(s.manager.redactText(data))
}

func (s *managedConsoleSession) appendSafeOutput(data string) error {
	if data == "" {
		return nil
	}
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.outputMu.Lock(ctx); err != nil {
		return err
	}
	defer s.outputMu.Unlock()
	s.mu.Lock()
	automationActive := s.activeExec != nil
	postAutomationFilter := !automationActive && time.Now().Before(s.filterUntil)
	keepShellPrompt := postAutomationFilter
	combinedRaw := s.rawTranscript + data
	retainedRaw := terminaltext.TailStringByBytes(combinedRaw, maxConsoleTranscriptLength)
	s.rawBaseOffset += int64(len(combinedRaw) - len(retainedRaw))
	s.rawTranscript = retainedRaw
	if automationActive {
		active := s.activeExec
		segment, _ := s.rawSegmentLocked(active.StartOffset)
		if strings.Contains(segment, "\n"+active.Marker+":") {
			keepShellPrompt = true
		}
	}
	displayData := data
	if automationActive || postAutomationFilter {
		displayData = terminaltext.CleanDisplayOutput(data, keepShellPrompt)
	}
	if displayData != "" {
		s.transcript = terminaltext.TailStringByBytes(s.transcript+displayData, maxConsoleTranscriptLength)
	}
	manualCompletion := s.manualOutputCompletionLocked()
	s.clearManualPauseIfPromptReturnedLocked()
	clients := maps.Clone(s.clients)
	s.mu.Unlock()
	if err := s.enqueueTranscript(s.ctx, displayData); err != nil {
		return err
	}
	if manualCompletion != nil {
		s.runOwnedWork(func() { s.finishManualOutputCapture(manualCompletion) })
	}
	if displayData != "" {
		s.broadcastTo(clients, ptyServerMessage{Type: "output", Status: "connected", Data: displayData, SessionID: s.id})
	}
	return nil
}

func (s *managedConsoleSession) rawStreamPositionLocked() int64 {
	return s.rawBaseOffset + int64(len(s.rawTranscript))
}

func (s *managedConsoleSession) rawSegmentLocked(startOffset int64) (string, bool) {
	return rawTranscriptSegment(s.rawTranscript, s.rawBaseOffset, startOffset)
}

func rawTranscriptSegment(transcript string, baseOffset int64, startOffset int64) (string, bool) {
	endOffset := baseOffset + int64(len(transcript))
	if startOffset < baseOffset {
		return transcript, true
	}
	if startOffset > endOffset {
		return "", true
	}
	return transcript[int(startOffset-baseOffset):], false
}

func (s *managedConsoleSession) appendDisplayOutput(ctx context.Context, data string) error {
	if data == "" {
		return nil
	}
	data = s.redactForPersistence(data)
	if err := s.outputMu.Lock(ctx); err != nil {
		return err
	}
	defer s.outputMu.Unlock()
	s.mu.Lock()
	if strings.HasPrefix(data, "[AI command]") && s.transcript != "" && !strings.HasSuffix(s.transcript, "\n") && !strings.HasSuffix(s.transcript, "\r") {
		data = "\r\n" + data
	}
	s.transcript = terminaltext.TailStringByBytes(s.transcript+data, maxConsoleTranscriptLength)
	clients := maps.Clone(s.clients)
	s.mu.Unlock()
	if err := s.enqueueTranscript(ctx, data); err != nil {
		return err
	}
	s.broadcastTo(clients, ptyServerMessage{Type: "output", Status: "connected", Data: data, SessionID: s.id})
	return nil
}

func (s *managedConsoleSession) enqueueTranscript(ctx context.Context, data string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	err := s.outputBuffer.Append(ctx, data, s.scheduleTranscriptFlush)
	if err != nil {
		logConsolePersistError("transcript_backpressure", s.id, err)
		return err
	}
	s.scheduleTranscriptFlush()
	return nil
}

func (s *managedConsoleSession) scheduleTranscriptFlush() {
	if s.manager == nil || s.manager.db == nil || !s.outputBuffer.StartWorker() {
		return
	}
	if !s.runOwnedWork(func() {
		s.outputBuffer.RunWorker(s.ctx, 500*time.Millisecond, func() error {
			err := s.flushTranscriptContext(context.Background())
			if err != nil {
				logConsolePersistError("flush_transcript", s.id, err)
			}
			return err
		})
	}) {
		s.outputBuffer.FinishWorker(true)
	}
}

func (s *managedConsoleSession) redactForPersistence(value string) string {
	if value == "" {
		return ""
	}
	s.mu.Lock()
	redactor := s.exactRedactor
	environment := s.environment
	closed := s.exactRedactionClosed
	s.mu.Unlock()
	if environment != nil && closed {
		return "[REDACTED VAULT VALUE]"
	}
	if redactor == nil && environment != nil {
		var err error
		redactor, err = environment.ExactValueRedactor()
		if err != nil {
			return "[REDACTED VAULT VALUE]"
		}
		defer redactor.Close()
	}
	if redactor == nil {
		return s.manager.redactText(value)
	}
	return actionresult.RedactCredentialText(value,
		func(text string) string { return string(redactor.Redact([]byte(text))) }, s.manager.redactText)
}

func (s *managedConsoleSession) closeExactRedactor() {
	s.mu.Lock()
	s.exactRedactionClosed = true
	s.mu.Unlock()
	if err := s.flushOutputStreams(); err != nil {
		s.finish("error", "console transcript persistence interrupted; output observation is incomplete")
	}
	s.mu.Lock()
	redactor := s.exactRedactor
	s.exactRedactor = nil
	s.stdoutExactRedactor = nil
	s.stderrExactRedactor = nil
	s.mu.Unlock()
	_ = redactor.Close()
}

func (s *managedConsoleSession) flushOutputStreams() error {
	s.mu.Lock()
	redactors := [2]*sessionenv.Redactor{s.stdoutExactRedactor, s.stderrExactRedactor}
	s.mu.Unlock()
	for _, data := range s.outputStreams.Close(redactors) {
		if err := s.appendSafeOutput(s.manager.redactText(data)); err != nil {
			return err
		}
	}
	return nil
}

func (s *managedConsoleSession) flushTranscript() {
	if err := s.flushTranscriptContext(context.Background()); err != nil {
		logConsolePersistError("flush_transcript", s.id, err)
	}
}

func (s *managedConsoleSession) flushTranscriptContext(ctx context.Context) error {
	if s.manager == nil || s.manager.db == nil {
		return errors.New("console persistence is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.outputBuffer.Drain(ctx, func(pending string) error {
		s.mu.Lock()
		snapshot := terminaltext.TailStringByBytes(s.transcript, maxConsoleSnapshotLength)
		s.mu.Unlock()
		persist := s.manager.persistChunks
		if persist == nil {
			persist = consolepersistence.PersistTranscript
		}
		return persist(ctx, s.manager.db, s.id, s.manager.redactText(snapshot), s.manager.redactText(pending), timeformat.Now())
	})
}

func (s *managedConsoleSession) finalize(ctx context.Context) error {
	if s == nil || s.manager == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.finalizeMu.Lock()
	defer s.finalizeMu.Unlock()
	if s.finalized {
		return nil
	}
	if !s.persisted {
		if err := s.flushTranscriptContext(ctx); err != nil {
			return fmt.Errorf("persist final console transcript: %w", err)
		}
		s.mu.Lock()
		status, message := s.finalStatus, s.finalMessage
		if status == "" {
			status, message = s.status, s.errText
		}
		s.mu.Unlock()
		now := timeformat.Now()
		persist := s.manager.persistStatus
		if persist == nil {
			persist = consolepersistence.PersistTerminalStatus
		}
		if err := persist(ctx, s.manager.db, s.id, status, message, now); err != nil {
			return fmt.Errorf("persist final console status: %w", err)
		}
		s.persisted = true
	}
	if !s.hookDone {
		s.manager.mu.Lock()
		hook := s.manager.sessionClosed
		s.manager.mu.Unlock()
		if hook != nil {
			if err := hook(ctx, s.handle()); err != nil {
				return fmt.Errorf("finalize console session ownership: %w", err)
			}
		}
		s.hookDone = true
	}
	s.manager.removeFinalized(s)
	s.finalized = true
	return nil
}

func logConsolePersistError(operation string, sessionID int64, err error) {
	log.Printf("console session persistence failed operation=%s session_id=%d error=%v", operation, sessionID, err)
}

func (s *managedConsoleSession) broadcast(message ptyServerMessage) {
	s.mu.Lock()
	clients := maps.Clone(s.clients)
	s.mu.Unlock()
	s.broadcastTo(clients, message)
}

func (s *managedConsoleSession) broadcastTo(clients map[*websocket.Conn]*sync.Mutex, message ptyServerMessage) {
	for ws, writeMu := range clients {
		if err := writePTYMessage(ws, writeMu, message); err != nil {
			s.removeClient(ws)
			_ = ws.Close()
		}
	}
}
