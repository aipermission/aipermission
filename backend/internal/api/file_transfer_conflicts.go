package api

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func (s fileTransferHandlers) checkUploadOverwrite(w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, runtimeID int64, remotePath string, overwrite bool, tempPath string) bool {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	adapter, err := s.fileTransferAdapter(ctx, runtime, runtimeID)
	if err != nil {
		_ = os.Remove(tempPath)
		handleConnectorTargetRuntimeError(w, err)
		return false
	}
	status, err := adapter.StatRemotePath(ctx, s.Server, runtime, runtimeID, remotePath)
	if err != nil {
		_ = os.Remove(tempPath)
		s.writeCredentialSafeConnectorError(w, ctx, runtime, runtimeID, adapter, http.StatusBadGateway, "remote path check failed", err)
		return false
	}
	if !status.Exists {
		return true
	}
	if status.Type != "file" {
		_ = os.Remove(tempPath)
		writeJSON(w, http.StatusConflict, remoteFileExistsResponse{
			Error:      "remote path already exists and is not a regular file",
			Code:       "remote_path_exists",
			RemotePath: remotePath,
			Type:       status.Type,
			Size:       status.Size,
		})
		return false
	}
	if !overwrite {
		_ = os.Remove(tempPath)
		writeJSON(w, http.StatusConflict, remoteFileExistsResponse{
			Error:      "remote file already exists",
			Code:       "remote_file_exists",
			RemotePath: remotePath,
			Type:       status.Type,
			Size:       status.Size,
		})
		return false
	}
	return true
}

func (s fileTransferHandlers) checkUploadBatchOverwrite(w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, runtimeID int64, requests []filetransfer.CreateRequest, overwrite bool, tempPaths []string) ([]remoteFileConflict, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	adapter, err := s.fileTransferAdapter(ctx, runtime, runtimeID)
	if err != nil {
		cleanupTempPaths(tempPaths)
		handleConnectorTargetRuntimeError(w, err)
		return nil, false
	}
	var conflicts []remoteFileConflict
	for _, item := range requests {
		status, err := adapter.StatRemotePath(ctx, s.Server, runtime, runtimeID, item.RemotePath)
		if err != nil {
			cleanupTempPaths(tempPaths)
			s.writeCredentialSafeConnectorError(w, ctx, runtime, runtimeID, adapter, http.StatusBadGateway, "remote path check failed", err)
			return nil, false
		}
		if !status.Exists {
			continue
		}
		if status.Type != "file" {
			cleanupTempPaths(tempPaths)
			writeJSON(w, http.StatusConflict, remoteFileConflictsResponse{
				Error: "one or more remote paths already exist and are not regular files",
				Code:  "remote_paths_exist",
				Conflicts: []remoteFileConflict{{
					RemotePath: item.RemotePath,
					Type:       status.Type,
					Size:       status.Size,
				}},
			})
			return nil, false
		}
		if !overwrite {
			conflicts = append(conflicts, remoteFileConflict{RemotePath: item.RemotePath, Type: status.Type, Size: status.Size})
		}
	}
	if len(conflicts) > 0 {
		cleanupTempPaths(tempPaths)
		return conflicts, false
	}
	return nil, true
}
