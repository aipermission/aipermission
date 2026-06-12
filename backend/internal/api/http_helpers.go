package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

type httpDomainError struct {
	NotFound        error
	NotFoundMessage string
	FailureMessage  string
	Validation      func(error) (string, bool)
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func (s *Server) activeRuntimeOrLocked(w http.ResponseWriter) (*databaseRuntime, bool) {
	runtime := s.activeRuntime()
	if runtime == nil {
		writeError(w, http.StatusLocked, "database is locked")
		return nil, false
	}
	return runtime, true
}

func parseInt64Query(w http.ResponseWriter, value string, name string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return id, true
}

func handleSSHKeyError(w http.ResponseWriter, err error) {
	handleDomainError(w, err, httpDomainError{
		NotFound:        sshkeys.ErrNotFound,
		NotFoundMessage: "ssh key not found",
		FailureMessage:  "ssh key operation failed",
		Validation: func(err error) (string, bool) {
			var validation sshkeys.ValidationError
			if errors.As(err, &validation) {
				return validation.Error(), true
			}
			return "", false
		},
	})
}

func handleTokenError(w http.ResponseWriter, err error) {
	handleDomainError(w, err, httpDomainError{
		NotFound:        tokens.ErrNotFound,
		NotFoundMessage: "token not found",
		FailureMessage:  "token operation failed",
		Validation: func(err error) (string, bool) {
			var validation tokens.ValidationError
			if errors.As(err, &validation) {
				return validation.Error(), true
			}
			return "", false
		},
	})
}

func handleDomainError(w http.ResponseWriter, err error, domain httpDomainError) {
	if errors.Is(err, domain.NotFound) {
		writeError(w, http.StatusNotFound, domain.NotFoundMessage)
		return
	}
	if domain.Validation != nil {
		if message, ok := domain.Validation(err); ok {
			writeError(w, http.StatusBadRequest, message)
			return
		}
	}
	if domain.FailureMessage != "" {
		writeError(w, http.StatusInternalServerError, domain.FailureMessage)
		return
	}
	writeInternalError(w)
}

func handleServerSSHMaterialError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, connectortargets.ErrTargetProfileNotFound), errors.Is(err, connectortargets.ErrTargetNotFound):
		writeError(w, http.StatusNotFound, "connector target profile not found")
	case errors.Is(err, sshkeys.ErrNotFound):
		handleSSHKeyError(w, err)
	default:
		writeInternalError(w)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
