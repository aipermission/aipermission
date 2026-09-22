package management

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func BrowseRemoteFilesAction(ctx context.Context, server connectorapi.RuntimeActionGateway, runtime connectorapi.ActionRuntime, runtimeID int64, remotePath string) (connectors.ActionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	target, privateKey, err := TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	entries, err := execution.ListRemoteDirectory(ctx, ExecutionTarget(server, target, privateKey), remotePath)
	if err != nil {
		return connectors.ActionResult{}, fmt.Errorf("%s", ConnectionFailureMessage(err))
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: browseRemoteFilesOutput(runtimeID, remotePath, entries),
	}, nil
}

func browseRemoteFilesOutput(runtimeID int64, remotePath string, entries []connectors.RemoteFileEntry) map[string]any {
	totalEntries := len(entries)
	truncated := totalEntries > sshconnector.MaxBrowseRemoteRows
	if truncated {
		entries = entries[:sshconnector.MaxBrowseRemoteRows]
	}
	return map[string]any{
		"runtime_id":    runtimeID,
		"path":          remotePath,
		"parent":        remotePathParent(remotePath),
		"entries":       entries,
		"max_rows":      sshconnector.MaxBrowseRemoteRows,
		"total_entries": totalEntries,
		"truncated":     truncated,
		"has_more":      truncated,
	}
}

func StartFileDownloadAction(ctx context.Context, server connectorapi.RuntimeActionGateway, runtimeContext connectors.RuntimeContext, runtimeID int64, remotePaths []string, archiveName string) (connectors.ActionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	batch, err := server.ConnectorCreateAndRunDownloadBatch(ctx, connectorapi.TransferAuthorization{
		ConnectorKind:         runtimeContext.Target.ConnectorKind,
		TargetID:              runtimeContext.Target.ID,
		TargetRef:             runtimeContext.Target.Ref,
		TargetUpdatedAt:       runtimeContext.Target.UpdatedAt,
		ProfileID:             runtimeContext.Profile.ID,
		ProfileUpdatedAt:      runtimeContext.Profile.UpdatedAt,
		ProfileSecretRevision: runtimeContext.Profile.SecretRevision,
	}, runtimeID, remotePaths, archiveName, filetransfer.SourceMCP)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"runtime_id": runtimeID,
			"batch_id":   batch.ID,
			"status":     batch.Status,
			"items":      batch.ItemCount,
		},
		DisplayText: "SSH download queue started.",
		Handles: connectors.ActionHandles{
			BatchID: batch.ID,
		},
	}, nil
}

func remotePathParent(remotePath string) string {
	if remotePath == "" || remotePath == "/" || remotePath == "." {
		return "/"
	}
	parent := path.Dir(remotePath)
	if parent == "." {
		return "/"
	}
	return parent
}
