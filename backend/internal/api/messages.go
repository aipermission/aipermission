package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

type messageRecord struct {
	ID         int64   `json:"id"`
	TokenID    int64   `json:"token_id"`
	TokenName  string  `json:"token_name,omitempty"`
	RuntimeID  *int64  `json:"runtime_id,omitempty"`
	TargetName string  `json:"target_name,omitempty"`
	SessionID  *int64  `json:"session_id,omitempty"`
	Direction  string  `json:"direction"`
	Message    string  `json:"message"`
	ConsumedAt *string `json:"consumed_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

type createMessageRequest struct {
	TokenID   int64  `json:"token_id"`
	RuntimeID *int64 `json:"runtime_id"`
	SessionID *int64 `json:"session_id"`
	Direction string `json:"direction"`
	Message   string `json:"message"`
}

type markMessagesReadRequest struct {
	RuntimeID int64 `json:"runtime_id"`
}

func (s messageHandlers) listMessages(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	filter := messageFilter{Direction: strings.TrimSpace(r.URL.Query().Get("direction"))}
	if rawRuntimeID := strings.TrimSpace(r.URL.Query().Get("runtime_id")); rawRuntimeID != "" {
		id, ok := parseInt64Query(w, rawRuntimeID, "runtime_id")
		if !ok {
			return
		}
		filter.RuntimeID = id
	}
	items, err := s.listMessageRecords(r.Context(), runtime, filter)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s messageHandlers) createMessage(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request createMessageRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	item, err := s.insertMessage(r.Context(), runtime, request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s messageHandlers) markMessagesRead(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request markMessagesReadRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if request.RuntimeID < 1 {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := runtime.database.ExecContext(r.Context(), `
		UPDATE message_queue
		SET consumed_at = ?
		WHERE direction = 'ai_to_user' AND consumed_at IS NULL AND runtime_id = ?`,
		now,
		request.RuntimeID,
	)
	if err != nil {
		writeInternalError(w)
		return
	}
	affected, _ := result.RowsAffected()
	writeJSON(w, http.StatusOK, map[string]any{"status": "read", "count": affected})
}

type messageFilter struct {
	TokenID   int64
	RuntimeID int64
	Direction string
}

func (s *Server) insertMessage(ctx context.Context, runtime *databaseRuntime, request createMessageRequest) (messageRecord, error) {
	request.Message = strings.TrimSpace(request.Message)
	request.Direction = strings.TrimSpace(request.Direction)
	if request.Direction == "" {
		request.Direction = "user_to_ai"
	}
	if request.Direction != "user_to_ai" && request.Direction != "ai_to_user" {
		return messageRecord{}, errors.New("direction must be user_to_ai or ai_to_user")
	}
	if request.TokenID < 1 {
		return messageRecord{}, errors.New("token_id is required")
	}
	if request.Message == "" {
		return messageRecord{}, errors.New("message is required")
	}
	if err := validateTextLimit("message", request.Message, maxMessageBytes); err != nil {
		return messageRecord{}, err
	}
	if err := validateMessageScope(ctx, runtime, &request); err != nil {
		return messageRecord{}, err
	}
	request.Message = s.redactForPersistence(ctx, runtime, request.Message)

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := runtime.database.ExecContext(ctx, `
		INSERT INTO message_queue (token_id, runtime_id, session_id, direction, message, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		request.TokenID,
		nullableInt64(request.RuntimeID),
		nullableInt64(request.SessionID),
		request.Direction,
		request.Message,
		now,
	)
	if err != nil {
		return messageRecord{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return messageRecord{}, err
	}
	return s.getMessageRecord(ctx, runtime, id)
}

func validateMessageScope(ctx context.Context, runtime *databaseRuntime, request *createMessageRequest) error {
	var tokenExists int
	err := runtime.database.QueryRowContext(ctx, `SELECT 1 FROM api_tokens WHERE id = ?`, request.TokenID).Scan(&tokenExists)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("token_id does not exist")
	}
	if err != nil {
		return err
	}

	if request.SessionID != nil {
		var sessionRuntimeID int64
		err := runtime.database.QueryRowContext(ctx, `SELECT runtime_id FROM console_sessions WHERE id = ?`, *request.SessionID).Scan(&sessionRuntimeID)
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("session_id does not exist")
		}
		if err != nil {
			return err
		}
		if request.RuntimeID != nil && *request.RuntimeID != sessionRuntimeID {
			return errors.New("session_id does not belong to runtime_id")
		}
		request.RuntimeID = &sessionRuntimeID
	}

	if request.RuntimeID != nil {
		if _, err := liveConsoleTargetRefForRuntimeID(ctx, runtime, *request.RuntimeID); err != nil {
			return errors.New("runtime_id does not exist")
		}
	}
	return nil
}

func (s *Server) consumeNextUserMessage(ctx context.Context, runtime *databaseRuntime, tokenID int64, runtimeID int64, sessionID int64) (*string, error) {
	tx, err := runtime.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
			SELECT id, message
			FROM message_queue
			WHERE token_id = ? AND direction = 'user_to_ai' AND consumed_at IS NULL
				AND ((? > 0 AND runtime_id = ?) OR runtime_id IS NULL)
				AND ((? > 0 AND session_id = ?) OR session_id IS NULL)
			ORDER BY
				CASE
					WHEN ? > 0 AND session_id = ? THEN 0
					WHEN runtime_id = ? THEN 1
					ELSE 2
				END,
				created_at ASC,
				id ASC
			LIMIT 1`,
		tokenID,
		runtimeID,
		runtimeID,
		sessionID,
		sessionID,
		sessionID,
		sessionID,
		runtimeID,
	)
	var id int64
	var message string
	if err := row.Scan(&id, &message); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.ExecContext(ctx, `
		UPDATE message_queue
		SET consumed_at = ?
		WHERE id = ? AND consumed_at IS NULL`,
		now,
		id,
	)
	if err != nil {
		return nil, err
	}
	if err := requireAffected(result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &message, nil
}

func (s *Server) nextUserMessage(ctx context.Context, runtime *databaseRuntime, tokenID int64, runtimeID int64, sessionID int64) (messageRecord, error) {
	row := runtime.database.QueryRowContext(ctx, messageSelectSQL()+`
			WHERE mq.token_id = ? AND mq.direction = 'user_to_ai' AND mq.consumed_at IS NULL
				AND ((? > 0 AND mq.runtime_id = ?) OR mq.runtime_id IS NULL)
				AND ((? > 0 AND mq.session_id = ?) OR mq.session_id IS NULL)
			ORDER BY
				CASE
					WHEN ? > 0 AND mq.session_id = ? THEN 0
					WHEN mq.runtime_id = ? THEN 1
					ELSE 2
				END,
				mq.created_at ASC
			LIMIT 1`,
		tokenID,
		runtimeID,
		runtimeID,
		sessionID,
		sessionID,
		sessionID,
		sessionID,
		runtimeID,
	)
	return scanMessage(row)
}

func consoleSessionRuntimeID(ctx context.Context, runtime *databaseRuntime, sessionID int64) (int64, error) {
	var runtimeID int64
	err := runtime.database.QueryRowContext(ctx, `SELECT runtime_id FROM console_sessions WHERE id = ?`, sessionID).Scan(&runtimeID)
	return runtimeID, err
}

func (s *Server) getMessageRecord(ctx context.Context, runtime *databaseRuntime, id int64) (messageRecord, error) {
	row := runtime.database.QueryRowContext(ctx, messageSelectSQL()+` WHERE mq.id = ?`, id)
	return scanMessage(row)
}

func (s *Server) listMessageRecords(ctx context.Context, runtime *databaseRuntime, filter messageFilter) ([]messageRecord, error) {
	where := []string{"(? = 0 OR mq.token_id = ?)", "(? = 0 OR mq.runtime_id = ?)", "(? = '' OR mq.direction = ?)"}
	args := []any{filter.TokenID, filter.TokenID, filter.RuntimeID, filter.RuntimeID, filter.Direction, filter.Direction}
	rows, err := runtime.database.QueryContext(ctx, messageSelectSQL()+`
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY mq.created_at DESC
		LIMIT 100`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []messageRecord{}
	for rows.Next() {
		item, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func messageSelectSQL() string {
	return `
			SELECT mq.id, mq.token_id, COALESCE(tok.name, ''), mq.runtime_id, COALESCE(ct.name, ''), mq.session_id,
			       mq.direction, mq.message, mq.consumed_at, mq.created_at
			FROM message_queue mq
			JOIN api_tokens tok ON tok.id = mq.token_id
			LEFT JOIN connector_runtime_surfaces rs ON rs.id = mq.runtime_id
			LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
			LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind`
}

func scanMessage(scanner interface {
	Scan(dest ...any) error
}) (messageRecord, error) {
	var item messageRecord
	var runtimeID sql.NullInt64
	var sessionID sql.NullInt64
	var consumedAt sql.NullString
	err := scanner.Scan(
		&item.ID,
		&item.TokenID,
		&item.TokenName,
		&runtimeID,
		&item.TargetName,
		&sessionID,
		&item.Direction,
		&item.Message,
		&consumedAt,
		&item.CreatedAt,
	)
	if err != nil {
		return messageRecord{}, err
	}
	if runtimeID.Valid {
		value := runtimeID.Int64
		item.RuntimeID = &value
	}
	if sessionID.Valid {
		value := sessionID.Int64
		item.SessionID = &value
	}
	if consumedAt.Valid {
		item.ConsumedAt = stringPtr(consumedAt.String)
	}
	return item, nil
}

func nullableInt64(value *int64) any {
	if value == nil || *value == 0 {
		return nil
	}
	return *value
}
