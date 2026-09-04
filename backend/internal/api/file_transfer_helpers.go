package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func (s fileTransferHandlers) fileTransferAdapter(ctx context.Context, runtime *databaseRuntime, runtimeID int64) (connectorapi.FileTransferAdapter, error) {
	store := connectortargets.NewStore(runtime.database)
	target, _, _, err := store.TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return nil, err
	}
	adapter := s.connectorFileTransferAdapterFor(target.ConnectorKind)
	if adapter == nil {
		return nil, connectortargets.ErrInvalidTargetRef
	}
	return adapter, nil
}

func writeConnectorError(w http.ResponseWriter, adapter any, err error) bool {
	presenter, _ := adapter.(connectorapi.ErrorPresenter)
	if presenter == nil {
		return false
	}
	return presenter.WriteConnectorError(w, err)
}

func connectorErrorMessage(adapter any, prefix string, err error) string {
	presenter, _ := adapter.(connectorapi.ErrorPresenter)
	if presenter != nil {
		return presenter.ConnectorErrorMessage(prefix, err)
	}
	if err == nil {
		return prefix
	}
	return prefix + ": " + strings.TrimSpace(err.Error())
}

func (s fileTransferHandlers) writeCredentialSafeConnectorError(
	w http.ResponseWriter,
	ctx context.Context,
	runtime *databaseRuntime,
	runtimeID int64,
	adapter any,
	status int,
	prefix string,
	err error,
) {
	boundary, boundaryErr := connectorCredentialBoundaryForRuntimeID(ctx, runtime, runtimeID)
	if boundaryErr != nil || err == nil || boundary.Redact(err.Error()) != err.Error() {
		writeError(w, status, prefix)
		return
	}
	presenter, _ := adapter.(connectorapi.ErrorPresenter)
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
	message := connectorErrorMessage(adapter, prefix, err)
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

func credentialSafeFileTransferErrorMessage(ctx context.Context, runtime *databaseRuntime, runtimeID int64, prefix string, adapter any, err error) string {
	if err == nil {
		return prefix
	}
	boundary, boundaryErr := connectorCredentialBoundaryForRuntimeID(ctx, runtime, runtimeID)
	if boundaryErr != nil || boundary.Redact(err.Error()) != err.Error() {
		return prefix
	}
	message := connectorErrorMessage(adapter, prefix, err)
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
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("remote_path is required")
	}
	if len([]rune(value)) > 4096 {
		return "", fmt.Errorf("remote_path must be 4096 characters or fewer")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("remote_path cannot contain control characters")
		}
	}
	if !path.IsAbs(value) {
		return "", fmt.Errorf("remote_path must be an absolute path")
	}
	cleaned := path.Clean(value)
	if cleaned == "/" || path.Base(cleaned) == "." {
		return "", fmt.Errorf("remote_path must point to a file")
	}
	return cleaned, nil
}

func normalizeRemoteDirectoryPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "/"
	}
	if len([]rune(value)) > 4096 {
		return "", fmt.Errorf("path must be 4096 characters or fewer")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("path cannot contain control characters")
		}
	}
	if !path.IsAbs(value) {
		return "", fmt.Errorf("path must be an absolute path")
	}
	return path.Clean(value), nil
}

func normalizeRelativeTransferPath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || len([]rune(value)) > 4096 {
		return "", fmt.Errorf("relative upload path is invalid")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("relative upload path cannot contain control characters")
		}
	}
	if path.IsAbs(value) {
		return "", fmt.Errorf("relative upload path must not be absolute")
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("relative upload path cannot leave the selected directory")
	}
	return cleaned, nil
}

func joinRemoteRelativePath(remoteDir string, relativePath string) string {
	if remoteDir == "/" {
		return "/" + strings.TrimLeft(relativePath, "/")
	}
	return strings.TrimRight(remoteDir, "/") + "/" + strings.TrimLeft(relativePath, "/")
}

func safeFileName(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = path.Base(value)
	value = strings.Trim(value, ". ")
	if value == "" || value == "/" || value == "." {
		return "aipermission-file"
	}
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			builder.WriteRune('_')
			continue
		}
		builder.WriteRune(r)
	}
	result := strings.TrimSpace(builder.String())
	if result == "" {
		return "aipermission-file"
	}
	if len([]rune(result)) > 160 {
		return string([]rune(result)[:160])
	}
	return result
}

func validFileTransferStatus(status string) bool {
	switch status {
	case filetransfer.StatusPending, filetransfer.StatusPendingApproval, filetransfer.StatusRunning, filetransfer.StatusPaused, filetransfer.StatusCompleted, filetransfer.StatusFailed, filetransfer.StatusCanceled:
		return true
	default:
		return false
	}
}
