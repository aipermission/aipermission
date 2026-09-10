package commandrequests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const (
	BulkMaxTargets         = 25
	BulkParallelism        = 3
	bulkDefaultReason      = "bulk console command"
	bulkMaxCommandBytes    = 64 << 10
	bulkMaxReasonBytes     = 2 << 10
	bulkInitialExecTimeout = 45 * time.Second
)

var ErrBulkTargetNotFound = errors.New("bulk command target not found")

type BulkTarget struct {
	RuntimeID int64
	Name      string
}

type BulkRequestOwner interface {
	Prepare(context.Context, Insert) (PreparedInsert, error)
	InsertPrepared(context.Context, Executor, PreparedInsert) (int64, error)
	SetSession(context.Context, int64, int64) error
	Finish(context.Context, Completion) error
	FinishActive(int64, executionprincipal.Principal, console.SessionHandle)
}

type BulkConsoleSessions interface {
	Exec(context.Context, executionprincipal.Principal, int64, string) (console.ExecResult, error)
}

type BulkAuditAppender func(*sql.Tx, string, *int64, int64, string, any) error
type BulkTransaction func(context.Context, func(*sql.Tx, BulkAuditAppender) error) error

type BulkHTTPRuntime struct {
	Requests        BulkRequestOwner
	Sessions        BulkConsoleSessions
	Principal       func() (executionprincipal.Principal, error)
	ResolveTarget   func(context.Context, int64) (BulkTarget, error)
	WithTransaction BulkTransaction
	PresentError    func(context.Context, int64, error) string
	InitialTimeout  time.Duration
}

type BulkHTTPScopeProvider func(http.ResponseWriter) (*BulkHTTPRuntime, bool)

type BulkHTTPHandlers struct {
	scope BulkHTTPScopeProvider
}

type BulkHTTPRequest struct {
	TargetIDs    []int64 `json:"target_ids"`
	Command      string  `json:"command"`
	Reason       string  `json:"reason"`
	Confirmation string  `json:"confirmation"`
}

type BulkHTTPResponse struct {
	Parallelism int                    `json:"parallelism"`
	Items       []BulkHTTPResponseItem `json:"items"`
}

type BulkHTTPResponseItem struct {
	RequestID  int64  `json:"request_id"`
	TargetID   int64  `json:"target_id"`
	TargetName string `json:"target_name"`
	Status     string `json:"status"`
}

func NewBulkHTTPHandlers(scope BulkHTTPScopeProvider) *BulkHTTPHandlers {
	return &BulkHTTPHandlers{scope: scope}
}

func (h *BulkHTTPHandlers) Run(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w)
	if !ok {
		return
	}
	var request BulkHTTPRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	request.Command = strings.TrimSpace(request.Command)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" {
		request.Reason = bulkDefaultReason
	}
	if err := validateBulkText("command", request.Command, bulkMaxCommandBytes); err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateBulkText("reason", request.Reason, bulkMaxReasonBytes); err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	targetIDs, err := normalizeBulkTargetIDs(request.TargetIDs)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	expectedConfirmation := BulkConfirmation(len(targetIDs))
	if request.Confirmation != expectedConfirmation {
		httptransport.WriteError(w, http.StatusBadRequest, "confirmation must be "+expectedConfirmation)
		return
	}

	targets := make([]BulkTarget, 0, len(targetIDs))
	for _, runtimeID := range targetIDs {
		target, err := runtime.ResolveTarget(r.Context(), runtimeID)
		if errors.Is(err, ErrBulkTargetNotFound) {
			httptransport.WriteError(w, http.StatusNotFound, "connector target profile not found")
			return
		}
		if err != nil {
			httptransport.WriteInternalError(w)
			return
		}
		if target.RuntimeID != runtimeID {
			httptransport.WriteInternalError(w)
			return
		}
		targets = append(targets, target)
	}

	prepared := make([]PreparedInsert, 0, len(targets))
	for _, target := range targets {
		item, err := runtime.Requests.Prepare(r.Context(), Insert{
			RuntimeID: target.RuntimeID, Source: SourceManual,
			Command: request.Command, Reason: request.Reason, Status: "running",
		})
		if err != nil {
			httptransport.WriteInternalError(w)
			return
		}
		prepared = append(prepared, item)
	}

	items := make([]BulkHTTPResponseItem, 0, len(targets))
	err = runtime.WithTransaction(r.Context(), func(tx *sql.Tx, appendAudit BulkAuditAppender) error {
		if appendAudit == nil {
			return errors.New("bulk command audit appender is unavailable")
		}
		for index, target := range targets {
			requestID, err := runtime.Requests.InsertPrepared(r.Context(), tx, prepared[index])
			if err != nil {
				return err
			}
			items = append(items, BulkHTTPResponseItem{
				RequestID: requestID, TargetID: target.RuntimeID, TargetName: target.Name, Status: "running",
			})
		}
		return appendAudit(tx, "user", nil, 0, "console.bulk_exec.started", map[string]any{
			"target_count": len(items), "request_ids": bulkRequestIDs(items), "command": request.Command,
		})
	})
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	runtime.run(request.Command, items)
	httptransport.WriteJSON(w, http.StatusAccepted, BulkHTTPResponse{Parallelism: BulkParallelism, Items: items})
}

func (h *BulkHTTPHandlers) resolve(w http.ResponseWriter) (*BulkHTTPRuntime, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	runtime, ok := h.scope(w)
	if !ok {
		return nil, false
	}
	if runtime == nil || runtime.Requests == nil || runtime.Sessions == nil || runtime.Principal == nil ||
		runtime.ResolveTarget == nil || runtime.WithTransaction == nil {
		httptransport.WriteInternalError(w)
		return nil, false
	}
	return runtime, true
}

func (runtime *BulkHTTPRuntime) run(command string, items []BulkHTTPResponseItem) {
	go func() {
		sem := make(chan struct{}, BulkParallelism)
		var wait sync.WaitGroup
		for _, item := range items {
			item := item
			wait.Add(1)
			go func() {
				defer wait.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				runtime.runOne(item, command)
			}()
		}
		wait.Wait()
	}()
}

func (runtime *BulkHTTPRuntime) runOne(item BulkHTTPResponseItem, command string) {
	timeout := runtime.InitialTimeout
	if timeout <= 0 {
		timeout = bulkInitialExecTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	principal, err := runtime.Principal()
	if err != nil || principal.Validate() != nil {
		if err == nil {
			err = executionprincipal.ErrInvalid
		}
		_ = runtime.Requests.Finish(context.Background(), Completion{ID: item.RequestID, Status: "error", Error: err.Error()})
		return
	}
	result, err := runtime.Sessions.Exec(ctx, principal, item.TargetID, command)
	if err != nil {
		message := "command execution failed: " + strings.TrimSpace(err.Error())
		if runtime.PresentError != nil {
			message = runtime.PresentError(context.Background(), item.TargetID, err)
		}
		if strings.TrimSpace(message) == "" {
			message = "command execution failed"
		}
		_ = runtime.Requests.Finish(context.Background(), Completion{ID: item.RequestID, Status: "error", Error: message})
		return
	}
	if result.Running {
		_ = runtime.Requests.SetSession(context.Background(), item.RequestID, result.SessionID)
		runtime.Requests.FinishActive(item.RequestID, principal, console.SessionHandle{
			ID: result.SessionID, RuntimeID: item.TargetID, Generation: result.Generation,
		})
		return
	}
	status := "completed"
	if result.ExitCode != 0 {
		status = "failed"
	}
	_ = runtime.Requests.Finish(context.Background(), Completion{
		ID: item.RequestID, Status: status, SessionID: result.SessionID,
		Stdout: result.Output, ExitCode: result.ExitCode,
	})
}

func normalizeBulkTargetIDs(values []int64) ([]int64, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("target_ids is required")
	}
	if len(values) > BulkMaxTargets {
		return nil, fmt.Errorf("target_ids must contain %d targets or fewer", BulkMaxTargets)
	}
	seen := map[int64]bool{}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value < 1 {
			return nil, fmt.Errorf("target_ids must contain positive ids")
		}
		if seen[value] {
			return nil, fmt.Errorf("target_ids must not contain duplicates")
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, nil
}

func BulkConfirmation(count int) string {
	return fmt.Sprintf("RUN ON %d TARGETS", count)
}

func bulkRequestIDs(items []BulkHTTPResponseItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.RequestID)
	}
	return ids
}

func validateBulkText(name, value string, maxBytes int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(value) > maxBytes {
		return fmt.Errorf("%s must be %d bytes or fewer", name, maxBytes)
	}
	return nil
}
