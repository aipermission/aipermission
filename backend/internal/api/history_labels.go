package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const defaultHistoryLabelColor = "#0f766e"

var historyLabelColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type createHistoryLabelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

type attachHistoryLabelRequest struct {
	LabelID int64  `json:"label_id,omitempty"`
	Name    string `json:"name,omitempty"`
	Color   string `json:"color,omitempty"`
}

func (s historyLabelHandlers) listHistoryLabels(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	labels, err := s.allHistoryLabels(r.Context(), runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

func (s historyLabelHandlers) createHistoryLabel(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request createHistoryLabelRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	label, created, err := s.createOrGetHistoryLabel(r.Context(), runtime, request.Name, request.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status := http.StatusOK
	action := "history.label.reused"
	if created {
		status = http.StatusCreated
		action = "history.label.created"
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, action, map[string]any{
		"label_id": label.ID,
		"name":     label.Name,
	})
	writeJSON(w, status, label)
}

func (s historyLabelHandlers) deleteHistoryLabel(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	labelID, ok := parsePathInt64(w, r, "id", "label id")
	if !ok {
		return
	}
	result, err := runtime.database.ExecContext(r.Context(), `DELETE FROM history_labels WHERE id = ?`, labelID)
	if err != nil {
		writeInternalError(w)
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeInternalError(w)
		return
	}
	if affected == 0 {
		writeError(w, http.StatusNotFound, "history label not found")
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "history.label.deleted", map[string]any{
		"label_id": labelID,
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s historyLabelHandlers) attachHistoryEntryLabel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request attachHistoryLabelRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	var label historyLabelRecord
	var created bool
	var err error
	if request.LabelID > 0 {
		label, err = s.getHistoryLabel(r.Context(), runtime, request.LabelID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
	} else {
		label, created, err = s.createOrGetHistoryLabel(r.Context(), runtime, request.Name, request.Color)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !historyEntryExists(r.Context(), runtime, id) {
		writeError(w, http.StatusNotFound, "history entry not found")
		return
	}
	if _, err := runtime.database.ExecContext(r.Context(), `
		INSERT OR IGNORE INTO history_entry_labels (history_entry_id, label_id, created_at)
		VALUES (?, ?, datetime('now'))`,
		id,
		label.ID,
	); err != nil {
		writeInternalError(w)
		return
	}
	labels, err := s.labelsForHistoryEntry(r.Context(), runtime, id)
	if err != nil {
		writeInternalError(w)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "history.label.attached", map[string]any{
		"history_entry_id": id,
		"label_id":         label.ID,
		"created":          created,
	})
	writeJSON(w, status, labels)
}

func (s historyLabelHandlers) detachHistoryEntryLabel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	labelID, ok := parsePathInt64(w, r, "label_id", "label id")
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	if !historyEntryExists(r.Context(), runtime, id) {
		writeError(w, http.StatusNotFound, "history entry not found")
		return
	}
	result, err := runtime.database.ExecContext(r.Context(), `
		DELETE FROM history_entry_labels
		WHERE history_entry_id = ? AND label_id = ?`,
		id,
		labelID,
	)
	if err != nil {
		writeInternalError(w)
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeInternalError(w)
		return
	}
	if affected == 0 {
		writeError(w, http.StatusNotFound, "history label relationship not found")
		return
	}
	labels, err := s.labelsForHistoryEntry(r.Context(), runtime, id)
	if err != nil {
		writeInternalError(w)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "history.label.detached", map[string]any{
		"history_entry_id": id,
		"label_id":         labelID,
	})
	writeJSON(w, http.StatusOK, labels)
}

func (s *Server) allHistoryLabels(ctx context.Context, runtime *databaseRuntime) ([]historyLabelRecord, error) {
	rows, err := runtime.database.QueryContext(ctx, `
		SELECT id, name, color, created_at, updated_at
		FROM history_labels
		ORDER BY lower(name), id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := []historyLabelRecord{}
	for rows.Next() {
		var label historyLabelRecord
		if err := rows.Scan(&label.ID, &label.Name, &label.Color, &label.CreatedAt, &label.UpdatedAt); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

func (s *Server) getHistoryLabel(ctx context.Context, runtime *databaseRuntime, id int64) (historyLabelRecord, error) {
	var label historyLabelRecord
	err := runtime.database.QueryRowContext(ctx, `
		SELECT id, name, color, created_at, updated_at
		FROM history_labels
		WHERE id = ?`,
		id,
	).Scan(&label.ID, &label.Name, &label.Color, &label.CreatedAt, &label.UpdatedAt)
	return label, err
}

func (s *Server) createOrGetHistoryLabel(ctx context.Context, runtime *databaseRuntime, name string, color string) (historyLabelRecord, bool, error) {
	name, err := normalizeHistoryLabelName(name)
	if err != nil {
		return historyLabelRecord{}, false, err
	}
	color = normalizeHistoryLabelColor(color)
	result, err := runtime.database.ExecContext(ctx, `
		INSERT OR IGNORE INTO history_labels (name, color, created_at, updated_at)
		VALUES (?, ?, datetime('now'), datetime('now'))`,
		name,
		color,
	)
	if err != nil {
		return historyLabelRecord{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return historyLabelRecord{}, false, err
	}
	var label historyLabelRecord
	err = runtime.database.QueryRowContext(ctx, `
		SELECT id, name, color, created_at, updated_at
		FROM history_labels
		WHERE name = ? COLLATE NOCASE`,
		name,
	).Scan(&label.ID, &label.Name, &label.Color, &label.CreatedAt, &label.UpdatedAt)
	return label, affected > 0, err
}

func historyEntryExists(ctx context.Context, runtime *databaseRuntime, id int64) bool {
	var exists int
	err := runtime.database.QueryRowContext(ctx, `SELECT 1 FROM history_entries WHERE id = ?`, id).Scan(&exists)
	return err == nil
}

func normalizeHistoryLabelName(name string) (string, error) {
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	if name == "" {
		return "", errors.New("label name is required")
	}
	if err := validateTextLimit("label name", name, 80); err != nil {
		return "", err
	}
	return name, nil
}

func normalizeHistoryLabelColor(color string) string {
	color = strings.TrimSpace(color)
	if !historyLabelColorPattern.MatchString(color) {
		return defaultHistoryLabelColor
	}
	return strings.ToLower(color)
}

func parsePathInt64(w http.ResponseWriter, r *http.Request, key string, label string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue(key)), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, label+" is required")
		return 0, false
	}
	return id, true
}
