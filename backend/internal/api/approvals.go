package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/console"
)

const (
	pendingApprovalAssistantHint = "Wait 3 seconds, then call get_request. Continue polling get_request until terminal status."
	runningAssistantHint         = "Wait 3 seconds, then call get_request again. Use read_console to inspect live output before sending another command to this server. If the request remains running and the console shows no useful progress, use restart_console_session(server_id) to recover the gateway-owned persistent console session."

	commandRequestSourceMCP    = "mcp"
	commandRequestSourceManual = "manual"
)

type commandRequestFilter struct {
	TokenID  int64
	Source   string
	Status   string
	ServerID int64
	LabelID  int64
	Query    string
	Limit    int
	Offset   int
}

type declineApprovalRequest struct {
	UserNote string `json:"user_note"`
}

type runApprovalRequest struct {
	UserNote string `json:"user_note"`
}

func (s approvalHandlers) listApprovals(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	filter := commandRequestFilter{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Source: strings.TrimSpace(r.URL.Query().Get("source")),
	}
	if filter.Source != "" && filter.Source != commandRequestSourceMCP && filter.Source != commandRequestSourceManual {
		writeError(w, http.StatusBadRequest, "invalid source")
		return
	}
	if rawServerID := strings.TrimSpace(r.URL.Query().Get("server_id")); rawServerID != "" {
		id, ok := parseInt64Query(w, rawServerID, "server_id")
		if !ok {
			return
		}
		filter.ServerID = id
	}
	if rawLabelID := strings.TrimSpace(r.URL.Query().Get("label_id")); rawLabelID != "" {
		id, ok := parseInt64Query(w, rawLabelID, "label_id")
		if !ok {
			return
		}
		filter.LabelID = id
	}

	if r.URL.Query().Get("paginated") == "true" {
		page, err := parsePageRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		filter.Query = page.Query
		filter.Limit = page.Limit
		filter.Offset = page.Offset
		items, total, err := s.listCommandRequestSummaries(r.Context(), runtime, filter)
		if err != nil {
			writeInternalError(w)
			return
		}
		writeJSON(w, http.StatusOK, makePageResponse(items, total, page))
		return
	}

	items, err := s.listCommandRequests(r.Context(), runtime, filter)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s approvalHandlers) getApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	item, err := s.getCommandRequest(r.Context(), runtime, id, 0, "")
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "command request not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s approvalHandlers) runApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request runApprovalRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.UserNote = strings.TrimSpace(request.UserNote)
	if err := validateTextLimit("user_note", request.UserNote, maxMessageBytes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	item, err := s.getCommandRequest(r.Context(), runtime, id, 0, "")
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "approval request not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	if item.Status != "pending_approval" {
		writeError(w, http.StatusConflict, fmt.Sprintf("approval request is %s", item.Status))
		return
	}
	if !runtime.isMCPStarted() {
		writeError(w, http.StatusConflict, "MCP execution is stopped; start MCP from the web UI before running approvals")
		return
	}

	command, err := s.commandRequestExecutionCommand(r.Context(), runtime, id)
	if err != nil {
		writeInternalError(w)
		return
	}
	drifted, driftReason, err := s.approvalContextDrift(r.Context(), runtime, item, command)
	if err != nil {
		writeInternalError(w)
		return
	}
	if drifted {
		if err := s.markCommandRequestStale(r.Context(), runtime, id, driftReason); err != nil {
			if errors.Is(err, errCommandRequestNotPending) {
				writeError(w, http.StatusConflict, "approval request is no longer pending")
				return
			}
			writeInternalError(w)
			return
		}
		s.writeAudit(r.Context(), runtime, "user", item.TokenID, item.ServerID, "approval.context_drift", map[string]any{
			"request_id": id,
			"reason":     driftReason,
		})
		writeError(w, http.StatusConflict, driftReason+"; ask the AI to send a fresh request")
		return
	}
	if request.UserNote != "" && item.TokenID != nil {
		_, err := s.insertMessage(r.Context(), runtime, createMessageRequest{
			TokenID:   *item.TokenID,
			ServerID:  &item.ServerID,
			SessionID: item.SessionID,
			Direction: "user_to_ai",
			Message:   request.UserNote,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := s.markCommandRequestRunning(r.Context(), runtime, id); err != nil {
		if errors.Is(err, errCommandRequestNotPending) {
			writeError(w, http.StatusConflict, "approval request is no longer pending")
			return
		}
		writeInternalError(w)
		return
	}
	go s.runApprovedCommand(runtime, id, item.ServerID, command)

	item.Status = "running"
	annotateCommandRequestForAssistant(&item)
	s.writeAudit(r.Context(), runtime, "user", item.TokenID, item.ServerID, "approval.run", map[string]any{
		"request_id": id,
		"command":    item.Command,
		"note":       request.UserNote != "",
	})
	writeJSON(w, http.StatusOK, item)
}

func (s approvalHandlers) declineApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}

	var request declineApprovalRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.UserNote = strings.TrimSpace(request.UserNote)

	item, err := s.getCommandRequest(r.Context(), runtime, id, 0, "")
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "approval request not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	if item.Status != "pending_approval" {
		writeError(w, http.StatusConflict, fmt.Sprintf("approval request is %s", item.Status))
		return
	}

	if err := s.declineCommandRequest(r.Context(), runtime, id, request.UserNote); err != nil {
		if errors.Is(err, errCommandRequestNotPending) {
			writeError(w, http.StatusConflict, "approval request is no longer pending")
			return
		}
		writeInternalError(w)
		return
	}
	item.Status = "declined"
	item.UserNote = stringPtr(request.UserNote)
	item.Error = "User declined the command"
	annotateCommandRequestForAssistant(&item)
	s.writeAudit(r.Context(), runtime, "user", item.TokenID, item.ServerID, "approval.decline", map[string]any{
		"request_id": id,
		"note":       request.UserNote != "",
	})
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) runApprovedCommand(runtime *databaseRuntime, requestID int64, serverID int64, command string) {
	ctx, cancel := context.WithTimeout(context.Background(), mcpInitialExecTimeout)
	defer cancel()

	result, err := runtime.consoleSessions.Exec(ctx, serverID, command)
	if err != nil {
		_ = s.finishCommandRequest(context.Background(), runtime, requestID, "error", 0, "", "", 0, err.Error())
		return
	}
	if result.Running {
		_ = s.setCommandRequestSession(context.Background(), runtime, requestID, result.SessionID)
		s.finishActiveCommandRequest(runtime, requestID, serverID)
		return
	}
	status := "completed"
	if result.ExitCode != 0 {
		status = "failed"
	}
	_ = s.finishCommandRequest(context.Background(), runtime, requestID, status, result.SessionID, result.Output, "", result.ExitCode, "")
}

func (s *Server) markCommandRequestRunning(ctx context.Context, runtime *databaseRuntime, id int64) error {
	result, err := runtime.database.ExecContext(ctx, `
		UPDATE command_requests
		SET status = 'running', error = '', completed_at = NULL
		WHERE id = ? AND status = 'pending_approval'`,
		id,
	)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Server) declineCommandRequest(ctx context.Context, runtime *databaseRuntime, id int64, userNote string) error {
	result, err := runtime.database.ExecContext(ctx, `
		UPDATE command_requests
		SET status = 'declined', user_note = ?, error = 'User declined the command', completed_at = datetime('now')
		WHERE id = ? AND status = 'pending_approval'`,
		userNote,
		id,
	)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

var errCommandRequestNotPending = errors.New("command request is not pending")

func requireAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errCommandRequestNotPending
	}
	return nil
}

func (s *Server) listCommandRequests(ctx context.Context, runtime *databaseRuntime, filter commandRequestFilter) ([]commandRequestRecord, error) {
	where := []string{"(? = '' OR cr.source = ?)", "(? = 0 OR cr.token_id = ?)", "(? = 0 OR cr.server_id = ?)", "(? = '' OR cr.status = ?)"}
	args := []any{filter.Source, filter.Source, filter.TokenID, filter.TokenID, filter.ServerID, filter.ServerID, filter.Status, filter.Status}
	if filter.LabelID != 0 {
		where = append(where, `cr.id IN (SELECT command_request_id FROM command_request_labels WHERE label_id = ?)`)
		args = append(args, filter.LabelID)
	}
	query := `
		SELECT cr.id, cr.token_id, COALESCE(tok.name, ''), cr.server_id, srv.name, cr.source, cr.command, cr.reason, cr.status,
		       cr.tracking_reason, cr.output_truncated, cr.stdout, cr.stderr, cr.exit_code, cr.session_id, cr.user_note, cr.error, cr.created_at, cr.completed_at
		FROM command_requests cr
		JOIN servers srv ON srv.id = cr.server_id
		LEFT JOIN api_tokens tok ON tok.id = cr.token_id
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY cr.created_at DESC, cr.id DESC
		LIMIT 100`
	rows, err := runtime.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []commandRequestRecord{}
	for rows.Next() {
		item, err := scanCommandRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachLabelsToCommandRequests(ctx, runtime, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) listCommandRequestSummaries(ctx context.Context, runtime *databaseRuntime, filter commandRequestFilter) ([]commandRequestRecord, int, error) {
	where := []string{"(? = '' OR cr.source = ?)", "(? = 0 OR cr.token_id = ?)", "(? = 0 OR cr.server_id = ?)", "(? = '' OR cr.status = ?)"}
	args := []any{filter.Source, filter.Source, filter.TokenID, filter.TokenID, filter.ServerID, filter.ServerID, filter.Status, filter.Status}
	if filter.LabelID != 0 {
		where = append(where, `cr.id IN (SELECT command_request_id FROM command_request_labels WHERE label_id = ?)`)
		args = append(args, filter.LabelID)
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		if ftsQuery := buildFTSQuery(filter.Query); ftsQuery != "" {
			where = append(where, `(cr.id IN (SELECT rowid FROM command_requests_fts WHERE command_requests_fts MATCH ?) OR srv.name LIKE ? OR COALESCE(tok.name, '') LIKE ?)`)
			args = append(args, ftsQuery, like, like)
		} else {
			where = append(where, `(cr.command LIKE ? OR cr.reason LIKE ? OR cr.status LIKE ? OR cr.source LIKE ? OR cr.tracking_reason LIKE ? OR cr.stdout LIKE ? OR cr.stderr LIKE ? OR cr.error LIKE ? OR srv.name LIKE ? OR COALESCE(tok.name, '') LIKE ?)`)
			args = append(args, like, like, like, like, like, like, like, like, like, like)
		}
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := runtime.database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM command_requests cr
		JOIN servers srv ON srv.id = cr.server_id
		LEFT JOIN api_tokens tok ON tok.id = cr.token_id
		WHERE `+whereSQL,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	queryArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := runtime.database.QueryContext(ctx, `
		SELECT cr.id, cr.token_id, COALESCE(tok.name, ''), cr.server_id, srv.name, cr.source, cr.command, cr.reason, cr.status,
		       cr.tracking_reason, cr.output_truncated, '' AS stdout, '' AS stderr, cr.exit_code, cr.session_id, cr.user_note, cr.error, cr.created_at, cr.completed_at
		FROM command_requests cr
		JOIN servers srv ON srv.id = cr.server_id
		LEFT JOIN api_tokens tok ON tok.id = cr.token_id
		WHERE `+whereSQL+`
		ORDER BY cr.created_at DESC, cr.id DESC
		LIMIT ? OFFSET ?`,
		queryArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []commandRequestRecord{}
	for rows.Next() {
		item, err := scanCommandRequest(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := s.attachLabelsToCommandRequests(ctx, runtime, items); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Server) getCommandRequest(ctx context.Context, runtime *databaseRuntime, id int64, tokenID int64, source string) (commandRequestRecord, error) {
	row := runtime.database.QueryRowContext(ctx, `
		SELECT cr.id, cr.token_id, COALESCE(tok.name, ''), cr.server_id, srv.name, cr.source, cr.command, cr.reason, cr.status,
		       cr.tracking_reason, cr.output_truncated, cr.stdout, cr.stderr, cr.exit_code, cr.session_id, cr.user_note, cr.error, cr.created_at, cr.completed_at
		FROM command_requests cr
		JOIN servers srv ON srv.id = cr.server_id
		LEFT JOIN api_tokens tok ON tok.id = cr.token_id
		WHERE cr.id = ? AND (? = '' OR cr.source = ?) AND (? = 0 OR cr.token_id = ?)`,
		id,
		source,
		source,
		tokenID,
		tokenID,
	)
	item, err := scanCommandRequest(row)
	if err != nil {
		return commandRequestRecord{}, err
	}
	labels, err := s.labelsForCommandRequest(ctx, runtime, item.ID)
	if err != nil {
		return commandRequestRecord{}, err
	}
	item.Labels = labels
	return item, nil
}

func scanCommandRequest(scanner interface {
	Scan(dest ...any) error
}) (commandRequestRecord, error) {
	var item commandRequestRecord
	var tokenID sql.NullInt64
	var exitCode sql.NullInt64
	var sessionID sql.NullInt64
	var userNote sql.NullString
	var completedAt sql.NullString
	var outputTruncated int
	err := scanner.Scan(
		&item.ID,
		&tokenID,
		&item.TokenName,
		&item.ServerID,
		&item.ServerName,
		&item.Source,
		&item.Command,
		&item.Reason,
		&item.Status,
		&item.TrackingReason,
		&outputTruncated,
		&item.Stdout,
		&item.Stderr,
		&exitCode,
		&sessionID,
		&userNote,
		&item.Error,
		&item.CreatedAt,
		&completedAt,
	)
	if err != nil {
		return commandRequestRecord{}, err
	}
	if item.Source == "" {
		item.Source = commandRequestSourceMCP
	}
	item.OutputTruncated = outputTruncated != 0
	item.Stdout = console.PlainOutput(item.Stdout)
	item.Stderr = console.PlainOutput(item.Stderr)
	if tokenID.Valid {
		value := tokenID.Int64
		item.TokenID = &value
	}
	if exitCode.Valid {
		value := int(exitCode.Int64)
		item.ExitCode = &value
	}
	if sessionID.Valid {
		value := sessionID.Int64
		item.SessionID = &value
	}
	if userNote.Valid {
		item.UserNote = stringPtr(userNote.String)
	}
	if completedAt.Valid {
		item.CompletedAt = stringPtr(completedAt.String)
	}
	annotateCommandRequestForAssistant(&item)
	return item, nil
}

func annotateCommandRequestForAssistant(item *commandRequestRecord) {
	item.RetryAfterSeconds = 0
	item.AssistantHint = ""
	if item.Status == "pending_approval" {
		item.RetryAfterSeconds = 3
		item.AssistantHint = pendingApprovalAssistantHint
		return
	}
	if item.Status == "running" {
		item.RetryAfterSeconds = 3
		item.AssistantHint = runningAssistantHint
	}
}

func stringPtr(value string) *string {
	return &value
}
