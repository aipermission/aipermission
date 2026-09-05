package api

import (
	"context"
	"errors"
	"mime"
	"mime/multipart"
	"net/http"
	"path"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func transferUploadFilename(adapter any, header *multipart.FileHeader) (string, error) {
	if _, exact := adapter.(connectorapi.FileTransferPathPolicy); exact {
		_, params, err := mime.ParseMediaType(header.Header.Get("Content-Disposition"))
		if err != nil {
			return "", err
		}
		return params["filename"], nil
	}
	return safeFileName(header.Filename), nil
}

func writeTransferPathError(w http.ResponseWriter, err error) {
	if errors.Is(err, connectortargets.ErrTargetProfileNotFound) || errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
		handleConnectorTargetRuntimeError(w, err)
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func (s fileTransferHandlers) normalizeTransferPath(ctx context.Context, runtime *databaseRuntime, runtimeID int64, value string, directory bool) (string, error) {
	adapter, err := s.fileTransferAdapter(ctx, runtime, runtimeID)
	if err != nil {
		return "", err
	}
	if policy, ok := adapter.(connectorapi.FileTransferPathPolicy); ok {
		return policy.NormalizeTransferPath(value, directory)
	}
	if directory {
		return normalizeRemoteDirectoryPath(value)
	}
	return normalizeRemoteFilePath(value)
}

func transferParent(adapter any, value string) string {
	if policy, ok := adapter.(connectorapi.FileTransferPathPolicy); ok {
		return policy.ParentTransferPath(value)
	}
	parent := path.Dir(value)
	if parent == "." || parent == value {
		return "/"
	}
	return parent
}

func transferUploadPath(adapter any, directory, relative string) (string, error) {
	if policy, ok := adapter.(connectorapi.FileTransferPathPolicy); ok {
		return policy.JoinTransferPath(directory, relative)
	}
	name, err := normalizeRelativeTransferPath(relative)
	if err != nil {
		return "", err
	}
	return joinRemoteRelativePath(directory, name), nil
}
