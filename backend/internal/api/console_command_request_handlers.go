package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/commandrequests"
)

func (s consoleHandlers) getConsoleCommandRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	item, err := commandrequests.NewStore(runtime.database).Get(r.Context(), id, 0, "")
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
