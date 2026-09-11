package filetransferhttp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func (s Handlers) writeCredentialSafeConnectorError(
	w http.ResponseWriter,
	execution transferExecution,
	status int,
	prefix string,
	err error,
) {
	boundary := execution.boundary
	if err == nil || boundary.Redact(err.Error()) != err.Error() {
		writeError(w, status, prefix)
		return
	}
	presenter, _ := execution.adapter.(connectorapi.ErrorPresenter)
	if presenter != nil {
		response := httptest.NewRecorder()
		if presenter.WriteConnectorError(response, err) {
			if connectorResponseContainsCredential(boundary, response.Result()) {
				writeError(w, status, prefix)
				return
			}
			for key, values := range response.Header() {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.WriteHeader(response.Code)
			_, _ = w.Write(response.Body.Bytes())
			return
		}
	}
	message := connectorapi.PresentedErrorMessage(execution.adapter, prefix, err)
	if boundary.Redact(message) != message {
		message = prefix
	}
	writeError(w, status, message)
}

func connectorResponseContainsCredential(boundary connectorCredentialBoundary, response *http.Response) bool {
	if response == nil || boundary.Redact(response.Header.Get("Location")) != response.Header.Get("Location") {
		return true
	}
	for key, values := range response.Header {
		if boundary.Redact(key) != key {
			return true
		}
		for _, value := range values {
			if boundary.Redact(value) != value {
				return true
			}
		}
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return true
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return boundary.Redact(string(body)) != string(body)
}

func connectorValueContainsCredential(boundary connectorCredentialBoundary, value any) bool {
	encoded, err := json.Marshal(value)
	if err != nil {
		return true
	}
	return boundary.Redact(string(encoded)) != string(encoded)
}

func credentialSafeFileTransferErrorMessage(execution *transferExecution, prefix string, err error) string {
	if err == nil || execution == nil {
		return prefix
	}
	boundary := execution.boundary
	if boundary.Redact(err.Error()) != err.Error() {
		return prefix
	}
	message := connectorapi.PresentedErrorMessage(execution.adapter, prefix, err)
	if boundary.Redact(message) != message {
		return prefix
	}
	return message
}

func parseFormInt64(w http.ResponseWriter, r *http.Request, field string) (int64, bool) {
	value := strings.TrimSpace(r.FormValue(field))
	if value == "" {
		writeError(w, http.StatusBadRequest, field+" is required")
		return 0, false
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid "+field)
		return 0, false
	}
	return id, true
}

func parseFormBool(r *http.Request, field string) bool {
	switch strings.ToLower(strings.TrimSpace(r.FormValue(field))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func normalizeRemoteFilePath(value string) (string, error) {
	return filetransfer.NormalizeRemoteFilePath(value)
}

func normalizeRemoteDirectoryPath(value string) (string, error) {
	return filetransfer.NormalizeRemoteDirectoryPath(value)
}

func normalizeRelativeTransferPath(value string) (string, error) {
	return filetransfer.NormalizeRelativeTransferPath(value)
}

func joinRemoteRelativePath(remoteDir, relativePath string) string {
	return filetransfer.JoinRemoteRelativePath(remoteDir, relativePath)
}

func safeFileName(value string) string {
	return filetransfer.SafeFileName(value)
}

func validFileTransferStatus(status string) bool {
	switch status {
	case filetransfer.StatusPending, filetransfer.StatusPendingApproval, filetransfer.StatusRunning, filetransfer.StatusPaused, filetransfer.StatusCompleted, filetransfer.StatusFailed, filetransfer.StatusCanceled:
		return true
	default:
		return false
	}
}
