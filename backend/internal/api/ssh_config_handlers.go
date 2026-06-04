package api

import (
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/sshconfig"
)

const maxSSHConfigParseBytes = 256 * 1024

type parseSSHConfigRequest struct {
	Content string `json:"content"`
}

func (s sshConfigHandlers) discoverSSHConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.activeRuntimeOrLocked(w); !ok {
		return
	}
	entries, err := sshconfig.DiscoverDefault()
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read gateway ssh config")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": entries})
}

func (s sshConfigHandlers) parseSSHConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.activeRuntimeOrLocked(w); !ok {
		return
	}
	var request parseSSHConfigRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.Content = strings.TrimSpace(request.Content)
	if request.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if len([]byte(request.Content)) > maxSSHConfigParseBytes {
		writeError(w, http.StatusBadRequest, "content is too large")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": sshconfig.Parse(request.Content)})
}
