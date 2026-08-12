// Package apiadapter registers S3 connector transfer services with the generic gateway.
package apiadapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	s3connector "github.com/aipermission/aipermission/backend/internal/connectors/s3"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type adapter struct{}

func init() {
	connectorapi.Register(s3connector.Kind, adapter{})
}

func (adapter) BrowseRemoteFiles(ctx context.Context, server connectorapi.GatewayServer, gatewayRuntime connectorapi.GatewayRuntime, runtimeID int64, remotePath string) ([]connectorapi.RemoteFileEntry, error) {
	runtime, err := transferRuntime(ctx, server, gatewayRuntime, runtimeID)
	if err != nil {
		return nil, err
	}
	entries, err := s3connector.BrowseRemoteFiles(ctx, runtime, remotePath)
	if err != nil {
		return nil, err
	}
	return remoteEntries(entries), nil
}

func (adapter) BrowseRemoteFilesPage(ctx context.Context, server connectorapi.GatewayServer, gatewayRuntime connectorapi.GatewayRuntime, runtimeID int64, remotePath string, cursor string) (connectorapi.RemoteFilePage, error) {
	runtime, err := transferRuntime(ctx, server, gatewayRuntime, runtimeID)
	if err != nil {
		return connectorapi.RemoteFilePage{}, err
	}
	page, err := s3connector.BrowseRemoteFilesPage(ctx, runtime, remotePath, cursor)
	if err != nil {
		return connectorapi.RemoteFilePage{}, err
	}
	return connectorapi.RemoteFilePage{Entries: remoteEntries(page.Entries), NextCursor: page.NextCursor, HasMore: page.HasMore}, nil
}

func (adapter) StatRemotePath(ctx context.Context, server connectorapi.GatewayServer, gatewayRuntime connectorapi.GatewayRuntime, runtimeID int64, remotePath string) (connectorapi.RemotePathStatus, error) {
	runtime, err := transferRuntime(ctx, server, gatewayRuntime, runtimeID)
	if err != nil {
		return connectorapi.RemotePathStatus{}, err
	}
	status, err := s3connector.StatRemotePath(ctx, runtime, remotePath)
	if err != nil {
		return connectorapi.RemotePathStatus{}, err
	}
	return connectorapi.RemotePathStatus{Exists: status.Exists, Type: status.Type, Size: status.Size}, nil
}

func (adapter) ListRecursiveFiles(ctx context.Context, server connectorapi.GatewayServer, gatewayRuntime connectorapi.GatewayRuntime, runtimeID int64, remotePath string, maxItems int, maxObjectBytes int64, maxBatchBytes int64) ([]connectorapi.RemoteFileEntry, error) {
	runtime, err := transferRuntime(ctx, server, gatewayRuntime, runtimeID)
	if err != nil {
		return nil, err
	}
	entries, err := s3connector.ListRecursiveFiles(ctx, runtime, remotePath, maxItems, maxObjectBytes, maxBatchBytes)
	if err != nil {
		if errors.Is(err, s3connector.ErrRemotePathNotFound) {
			return nil, fmt.Errorf("%w: %v", connectorapi.ErrRemotePathNotFound, err)
		}
		if errors.Is(err, s3connector.ErrTransferLimit) {
			return nil, fmt.Errorf("%w: %v", connectorapi.ErrTransferLimit, err)
		}
		return nil, err
	}
	return remoteEntries(entries), nil
}

func (adapter) UploadFile(ctx context.Context, server connectorapi.GatewayServer, gatewayRuntime connectorapi.GatewayRuntime, runtimeID int64, localPath string, remotePath string, overwrite bool, options connectorapi.TransferOptions) (connectorapi.TransferResult, error) {
	runtime, err := transferRuntime(ctx, server, gatewayRuntime, runtimeID)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	result, err := s3connector.UploadFile(ctx, runtime, localPath, remotePath, overwrite, s3TransferOptions(options))
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	return transferResult(result), nil
}

func (adapter) DownloadFile(ctx context.Context, server connectorapi.GatewayServer, gatewayRuntime connectorapi.GatewayRuntime, runtimeID int64, remotePath string, localPath string, options connectorapi.TransferOptions) (connectorapi.TransferResult, error) {
	runtime, err := transferRuntime(ctx, server, gatewayRuntime, runtimeID)
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	result, err := s3connector.DownloadFile(ctx, runtime, remotePath, localPath, s3TransferOptions(options))
	if err != nil {
		return connectorapi.TransferResult{}, err
	}
	return transferResult(result), nil
}

type secretAccessor map[string]any

func (values secretAccessor) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := values[name]
	if !ok || value == nil {
		return "", fmt.Errorf("%w: %q", connectors.ErrSecretNotFound, name)
	}
	if text, ok := value.(string); ok {
		return text, nil
	}
	return fmt.Sprint(value), nil
}

func transferRuntime(ctx context.Context, server connectorapi.GatewayServer, runtime connectorapi.GatewayRuntime, runtimeID int64) (connectors.RuntimeContext, error) {
	if server == nil || runtime == nil || runtime.ConnectorDatabase() == nil || runtime.ConnectorVault() == nil {
		return connectors.RuntimeContext{}, fmt.Errorf("s3 transfer runtime is unavailable")
	}
	store := connectortargets.NewStore(runtime.ConnectorDatabase())
	target, profile, surface, err := store.TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return connectors.RuntimeContext{}, err
	}
	if surface.ConnectorKind != s3connector.Kind || surface.CapabilityKind != connectortargets.RuntimeCapabilityFileTransfer {
		return connectors.RuntimeContext{}, connectortargets.ErrRuntimeSurfaceNotFound
	}
	storedProfile, err := store.GetCredentialProfile(ctx, surface.TargetID, surface.ProfileID)
	if err != nil {
		return connectors.RuntimeContext{}, err
	}
	secrets := map[string]any{}
	if err := runtime.ConnectorVault().DecryptJSON(storedProfile.EncryptedSecretJSON, &secrets); err != nil {
		return connectors.RuntimeContext{}, fmt.Errorf("decrypt s3 credential profile: %w", err)
	}
	capabilityServer, ok := server.(connectorapi.RuntimeCapabilityServer)
	if !ok {
		return connectors.RuntimeContext{}, fmt.Errorf("s3 transfer runtime capabilities are unavailable")
	}
	return connectors.RuntimeContext{
		Target:       target,
		Profile:      profile,
		Secrets:      secretAccessor(secrets),
		Capabilities: capabilityServer.ConnectorRuntimeCapabilities(s3connector.Kind, runtime),
	}, nil
}

func remoteEntries(entries []s3connector.RemoteFileEntry) []connectorapi.RemoteFileEntry {
	result := make([]connectorapi.RemoteFileEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, connectorapi.RemoteFileEntry{Name: entry.Name, Path: entry.Path, Type: entry.Type, Size: entry.Size, ModifiedAt: entry.ModifiedAt})
	}
	return result
}

func s3TransferOptions(options connectorapi.TransferOptions) s3connector.TransferOptions {
	return s3connector.TransferOptions{Progress: s3connector.TransferProgress(options.Progress), Wait: options.Wait, MaxBytes: options.MaxBytes}
}

func transferResult(result s3connector.TransferResult) connectorapi.TransferResult {
	return connectorapi.TransferResult{Bytes: result.Bytes, Size: result.Size, ChecksumSHA256: result.ChecksumSHA256, DurationMS: result.DurationMS}
}
