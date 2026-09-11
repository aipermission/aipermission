package filetransferhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
)

type downloadBatchPlan struct {
	remotePaths []string
	fileNames   []string
	archiveName string
}

func validateDownloadBatchInput(runtimeID int64, remotePaths []string, source string, idempotencyKey string) error {
	if runtimeID < 1 {
		return newFileTransferStartError(http.StatusBadRequest, "runtime_id is required")
	}
	if len(remotePaths) == 0 {
		return newFileTransferStartError(http.StatusBadRequest, "remote_paths is required")
	}
	if len(remotePaths) > maxFileTransferBatchItems {
		return newFileTransferStartError(http.StatusBadRequest, fmt.Sprintf("cannot download more than %d files at once", maxFileTransferBatchItems))
	}
	if source == filetransfer.SourceUI && strings.TrimSpace(idempotencyKey) == "" {
		return newFileTransferStartError(http.StatusBadRequest, "idempotency_key is required")
	}
	if len(strings.TrimSpace(idempotencyKey)) > filetransfer.MaxIdempotencyKeyBytes {
		return newFileTransferStartError(http.StatusBadRequest, "idempotency_key is too long")
	}
	for _, remotePath := range remotePaths {
		if err := validateTransferPathSyntax(remotePath); err != nil {
			return newFileTransferStartError(http.StatusBadRequest, err.Error())
		}
	}
	return nil
}

func (s Handlers) downloadBatchExecution(ctx context.Context, runtime *transferapp.Runtime, accepted *transferExecution, runtimeID int64) (transferExecution, error) {
	if accepted == nil {
		return s.resolveTransferExecution(ctx, runtime, runtimeID)
	}
	if accepted.runtime == nil || accepted.runtimeID != runtimeID {
		return transferExecution{}, errTransferExecutionStale
	}
	return *accepted, nil
}

func prepareDownloadBatchPlan(adapter connectorapi.FileTransferAdapter, remotePaths []string, archiveName string) (downloadBatchPlan, error) {
	if policy, ok := adapter.(connectorapi.FileTransferPathPolicy); ok && (len(remotePaths) > 1 || archiveName != "") {
		if err := policy.ValidateDownloadPaths(remotePaths); err != nil {
			return downloadBatchPlan{}, newFileTransferStartError(http.StatusBadRequest, err.Error())
		}
	}
	plan := downloadBatchPlan{
		remotePaths: make([]string, 0, len(remotePaths)),
		fileNames:   make([]string, 0, len(remotePaths)),
	}
	seen := map[string]bool{}
	for _, raw := range remotePaths {
		remotePath, err := normalizeTransferPathForAdapter(adapter, raw, false)
		if err != nil {
			return downloadBatchPlan{}, newFileTransferStartError(http.StatusBadRequest, err.Error())
		}
		if seen[remotePath] {
			return downloadBatchPlan{}, newFileTransferStartError(http.StatusBadRequest, "download queue contains duplicate remote paths")
		}
		fileName := safeFileName(path.Base(remotePath))
		if strings.TrimSpace(fileName) == "" {
			fileName = "aipermission-file"
		}
		if err := filetransfer.ValidateFileName(fileName); err != nil {
			return downloadBatchPlan{}, newFileTransferStartError(http.StatusBadRequest, "remote path cannot be represented as a local filename")
		}
		seen[remotePath] = true
		plan.remotePaths = append(plan.remotePaths, remotePath)
		plan.fileNames = append(plan.fileNames, fileName)
	}
	if strings.TrimSpace(archiveName) != "" {
		plan.archiveName = safeFileName(archiveName)
	}
	return plan, nil
}

func (plan *downloadBatchPlan) assignDefaultArchiveName() {
	if plan.archiveName == "" && len(plan.remotePaths) > 1 {
		plan.archiveName = fmt.Sprintf("aipermission-download-%s.zip", time.Now().UTC().Format("20060102-150405"))
	}
}

func downloadBatchIdempotency(ctx context.Context, runtime *transferapp.Runtime, runtimeID int64, source string, idempotencyKey string, plan downloadBatchPlan) (filetransfer.IdempotencyClaim, *filetransfer.BatchRecord, error) {
	if source != filetransfer.SourceUI {
		return filetransfer.IdempotencyClaim{}, nil, nil
	}
	claim, err := fileTransferStartClaim(idempotencyKey, filetransfer.IdempotencyResourceBatch, struct {
		RuntimeID   int64    `json:"runtime_id"`
		Direction   string   `json:"direction"`
		RemotePaths []string `json:"remote_paths"`
		ArchiveName string   `json:"archive_name"`
	}{runtimeID, filetransfer.DirectionDownload, plan.remotePaths, plan.archiveName})
	if err != nil {
		return filetransfer.IdempotencyClaim{}, nil, err
	}
	replay, err := runtime.Storage().GetIdempotentBatch(ctx, claim)
	if err == nil {
		return claim, &replay, nil
	}
	if !errors.Is(err, filetransfer.ErrIdempotencyNotFound) {
		return filetransfer.IdempotencyClaim{}, nil, err
	}
	return claim, nil, nil
}

func (s Handlers) prepareDownloadBatchItems(ctx context.Context, execution transferExecution, runtimeID int64, plan downloadBatchPlan, validateRemote bool) (_ []filetransfer.CreateRequest, tempPaths []string, resultErr error) {
	defer func() {
		if resultErr != nil {
			cleanupTempPaths(tempPaths)
		}
	}()
	items := make([]filetransfer.CreateRequest, 0, len(plan.remotePaths))
	var totalSize int64
	for index, remotePath := range plan.remotePaths {
		size, err := statDownloadBatchItem(ctx, execution, runtimeID, remotePath, validateRemote)
		if err != nil {
			return nil, tempPaths, err
		}
		if totalSize > maxFileTransferBatchBytes-size {
			return nil, tempPaths, newFileTransferStartError(http.StatusRequestEntityTooLarge, "download batch cannot exceed "+filetransfer.FormatByteLimit(maxFileTransferBatchBytes)+" total size")
		}
		totalSize += size
		tempPath, err := s.runner.ReserveDownloadTempFile()
		if err != nil {
			return nil, tempPaths, err
		}
		tempPaths = append(tempPaths, tempPath)
		items = append(items, filetransfer.CreateRequest{
			RemotePath: remotePath,
			FileName:   plan.fileNames[index],
			SizeBytes:  size,
			TempPath:   tempPath,
		})
	}
	return items, tempPaths, nil
}

func statDownloadBatchItem(ctx context.Context, execution transferExecution, runtimeID int64, remotePath string, validateRemote bool) (int64, error) {
	if !validateRemote {
		return 0, nil
	}
	status, err := execution.adapter.StatRemotePath(ctx, execution.gateway, execution.runtime, runtimeID, remotePath)
	if err != nil {
		return 0, newFileTransferConnectorError(execution, err)
	}
	if !status.Exists || status.Type != "file" {
		return 0, newFileTransferStartError(http.StatusBadRequest, "remote path must be an existing regular file")
	}
	if err := validateDownloadObjectSize(status.Size); err != nil {
		return 0, newFileTransferStartError(http.StatusRequestEntityTooLarge, err.Error())
	}
	return status.Size, nil
}
