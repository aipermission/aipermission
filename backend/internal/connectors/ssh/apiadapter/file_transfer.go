package apiadapter

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
)

func (adapter) BrowseRemoteFiles(ctx context.Context, server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, runtimeID int64, remotePath string) ([]connectorapi.RemoteFileEntry, error) {
	gateway, err := serverFrom(server)
	if err != nil {
		return nil, err
	}
	target, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		return nil, err
	}
	entries, err := execution.ListRemoteDirectory(ctx, executionTarget(gateway, target, privateKey), remotePath)
	if err != nil {
		return nil, err
	}
	return remoteFileEntries(entries), nil
}

func (adapter) StatRemotePath(ctx context.Context, server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, runtimeID int64, remotePath string) (connectorapi.RemotePathStatus, error) {
	gateway, err := serverFrom(server)
	if err != nil {
		return connectorapi.RemotePathStatus{}, err
	}
	target, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		return connectorapi.RemotePathStatus{}, err
	}
	status, err := execution.StatRemotePath(ctx, executionTarget(gateway, target, privateKey), remotePath)
	if err != nil {
		return connectorapi.RemotePathStatus{}, err
	}
	return connectorapi.RemotePathStatus{Exists: status.Exists, Type: status.Type, Size: status.Size}, nil
}

func (adapter) UploadFile(ctx context.Context, server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, runtimeID int64, localPath string, remotePath string, overwrite bool, options connectorapi.TransferOptions) (connectorapi.TransferResult, error) {
	gateway, err := serverFrom(server)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	target, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	result, err := execution.UploadFileWithOptions(ctx, executionTarget(gateway, target, privateKey), localPath, remotePath, overwrite, executionTransferOptions(options))
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	return connectorTransferResult(result), nil
}

func (adapter) DownloadFile(ctx context.Context, server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, runtimeID int64, remotePath string, localPath string, options connectorapi.TransferOptions) (connectorapi.TransferResult, error) {
	gateway, err := serverFrom(server)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	target, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	result, err := execution.DownloadFileWithOptions(ctx, executionTarget(gateway, target, privateKey), remotePath, localPath, executionTransferOptions(options))
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	return connectorTransferResult(result), nil
}
