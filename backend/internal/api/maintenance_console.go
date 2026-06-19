package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maintenanceConsoleMaxCommandBytes = 8 << 10
	maintenanceConsoleMaxOutputBytes  = 64 << 10
	maintenanceConsoleDefaultTimeout  = 10
	maintenanceConsoleMaxTimeout      = 30
)

type maintenanceConsoleRunRequest struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type maintenanceConsoleRunResponse struct {
	Status          string `json:"status"`
	Command         string `json:"command"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        *int   `json:"exit_code,omitempty"`
	TimedOut        bool   `json:"timed_out"`
	OutputTruncated bool   `json:"output_truncated"`
	DurationMs      int64  `json:"duration_ms"`
	StartedAt       string `json:"started_at"`
	FinishedAt      string `json:"finished_at"`
}

func (h maintenanceConsoleHandlers) status(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.activeRuntimeOrLocked(w); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":             true,
		"scope":               "local-ui-only",
		"shell":               "/bin/sh -lc",
		"max_command_bytes":   maintenanceConsoleMaxCommandBytes,
		"max_output_bytes":    maintenanceConsoleMaxOutputBytes,
		"default_timeout_sec": maintenanceConsoleDefaultTimeout,
		"max_timeout_sec":     maintenanceConsoleMaxTimeout,
	})
}

func (h maintenanceConsoleHandlers) open(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	h.writeAudit(r.Context(), runtime, "user", nil, 0, "maintenance_console.opened", map[string]any{
		"scope": "local-ui-only",
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"opened":    true,
		"opened_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h maintenanceConsoleHandlers) close(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	h.writeAudit(r.Context(), runtime, "user", nil, 0, "maintenance_console.closed", map[string]any{
		"scope": "local-ui-only",
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"closed":    true,
		"closed_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h maintenanceConsoleHandlers) run(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request maintenanceConsoleRunRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	command := strings.TrimSpace(request.Command)
	if command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}
	if err := validateTextLimit("command", command, maintenanceConsoleMaxCommandBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	timeout := request.TimeoutSeconds
	if timeout <= 0 {
		timeout = maintenanceConsoleDefaultTimeout
	}
	if timeout > maintenanceConsoleMaxTimeout {
		writeError(w, http.StatusBadRequest, "timeout_seconds must be 30 or less")
		return
	}

	started := time.Now().UTC()
	h.writeAudit(r.Context(), runtime, "user", nil, 0, "maintenance_console.command.started", map[string]any{
		"command":         command,
		"timeout_seconds": timeout,
	})
	result := runMaintenanceConsoleCommand(r.Context(), command, timeout)
	h.writeAudit(r.Context(), runtime, "user", nil, 0, "maintenance_console.command.finished", map[string]any{
		"command":          command,
		"status":           result.Status,
		"exit_code":        result.ExitCode,
		"timed_out":        result.TimedOut,
		"output_truncated": result.OutputTruncated,
		"duration_ms":      result.DurationMs,
	})
	result.StartedAt = started.Format(time.RFC3339)
	writeJSON(w, http.StatusOK, result)
}

func runMaintenanceConsoleCommand(parent context.Context, command string, timeoutSeconds int) maintenanceConsoleRunResponse {
	started := time.Now().UTC()
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	stdout := newLimitedBuffer(maintenanceConsoleMaxOutputBytes)
	stderr := newLimitedBuffer(maintenanceConsoleMaxOutputBytes)
	cmd := exec.Command("/bin/sh", "-lc", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Start()
	if err == nil {
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()
		select {
		case err = <-done:
		case <-ctx.Done():
			killProcessGroup(cmd)
			err = <-done
		}
	}
	finished := time.Now().UTC()

	response := maintenanceConsoleRunResponse{
		Status:          "completed",
		Command:         command,
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		TimedOut:        errors.Is(ctx.Err(), context.DeadlineExceeded),
		OutputTruncated: stdout.Truncated() || stderr.Truncated(),
		DurationMs:      finished.Sub(started).Milliseconds(),
		StartedAt:       started.Format(time.RFC3339),
		FinishedAt:      finished.Format(time.RFC3339),
	}
	if response.TimedOut {
		response.Status = "timed_out"
		return response
	}
	if err != nil {
		response.Status = "failed"
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitCode()
			response.ExitCode = &code
			return response
		}
		if response.Stderr == "" {
			response.Stderr = err.Error()
		}
		return response
	}
	code := 0
	response.ExitCode = &code
	return response
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if cmd.Process.Pid > 0 {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	_ = cmd.Process.Kill()
}

type limitedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit <= 0 {
		b.truncated = true
		return len(p), nil
	}
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.buffer.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	b.buffer.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func (b *limitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}
