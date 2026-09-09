package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/history"
)

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
	labels, err := history.NewLabelStore(runtime.database).All(r.Context())
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
	if !decodeJSON(w, r, &request) {
		return
	}
	var label historyLabelRecord
	var created bool
	err := s.withAuditedMutation(
		r.Context(), runtime, "user", nil, 0, "history.label.created",
		func() any { return map[string]any{"label_id": label.ID, "name": label.Name} },
		func(tx *sql.Tx) error {
			var err error
			label, created, err = history.NewLabelStore(tx).CreateOrGet(r.Context(), request.Name, request.Color)
			if err == nil && !created {
				return errAuditedMutationUnchanged
			}
			return err
		},
	)
	if errors.Is(err, errAuditedMutationUnchanged) {
		label, err = history.NewLabelStore(runtime.database).GetByName(r.Context(), request.Name)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
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
	err := s.withAuditedMutation(
		r.Context(), runtime, "user", nil, 0, "history.label.deleted",
		func() any { return map[string]any{"label_id": labelID} },
		func(tx *sql.Tx) error {
			result, err := tx.ExecContext(r.Context(), `DELETE FROM history_labels WHERE id = ?`, labelID)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err == nil && affected == 0 {
				return sql.ErrNoRows
			}
			return err
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "history label not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
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
	if !decodeJSON(w, r, &request) {
		return
	}
	if !history.NewLabelStore(runtime.database).EntryExists(r.Context(), id) {
		writeError(w, http.StatusNotFound, "history entry not found")
		return
	}
	var label historyLabelRecord
	var created bool
	err := s.withAuditedMutation(
		r.Context(), runtime, "user", nil, 0, "history.label.attached",
		func() any {
			return map[string]any{"history_entry_id": id, "label_id": label.ID, "created": created}
		},
		func(tx *sql.Tx) error {
			var err error
			if request.LabelID > 0 {
				label, err = history.NewLabelStore(tx).Get(r.Context(), request.LabelID)
			} else {
				label, created, err = history.NewLabelStore(tx).CreateOrGet(r.Context(), request.Name, request.Color)
			}
			if err != nil {
				return err
			}
			result, err := tx.ExecContext(r.Context(), `
				INSERT OR IGNORE INTO history_entry_labels (history_entry_id, label_id, created_at)
				VALUES (?, ?, datetime('now'))`, id, label.ID)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err == nil && affected == 0 && !created {
				return errAuditedMutationUnchanged
			}
			return err
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "label not found")
		return
	}
	if errors.Is(err, errAuditedMutationUnchanged) {
		err = nil
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	labels, err := history.NewQueryStore(runtime.database).Labels(r.Context(), id)
	if err != nil {
		writeInternalError(w)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
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
	if !history.NewLabelStore(runtime.database).EntryExists(r.Context(), id) {
		writeError(w, http.StatusNotFound, "history entry not found")
		return
	}
	err := s.withAuditedMutation(
		r.Context(), runtime, "user", nil, 0, "history.label.detached",
		func() any { return map[string]any{"history_entry_id": id, "label_id": labelID} },
		func(tx *sql.Tx) error {
			result, err := tx.ExecContext(r.Context(), `
				DELETE FROM history_entry_labels
				WHERE history_entry_id = ? AND label_id = ?`, id, labelID)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err == nil && affected == 0 {
				return sql.ErrNoRows
			}
			return err
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "history label relationship not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	labels, err := history.NewQueryStore(runtime.database).Labels(r.Context(), id)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

func parsePathInt64(w http.ResponseWriter, r *http.Request, key string, label string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue(key)), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, label+" is required")
		return 0, false
	}
	return id, true
}
