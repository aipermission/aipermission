package api

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func (s fileTransferHandlers) fileTransferAdapter(ctx context.Context, runtime *databaseRuntime, runtimeID int64) (connectorapi.FileTransferAdapter, error) {
	targetRef, err := liveConsoleTargetRefForRuntimeID(ctx, runtime, runtimeID)
	if err != nil {
		return nil, err
	}
	target, _, err := connectortargets.NewStore(runtime.database).ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return nil, err
	}
	adapter := connectorFileTransferAdapterFor(target.ConnectorKind)
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
