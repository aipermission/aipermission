package apiadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
)

func targetConfigFromConnectorConfig(config map[string]any) (map[string]any, error) {
	allowed := map[string]bool{
		"host":                        true,
		"port":                        true,
		"description":                 true,
		"startup_input_after_connect": true,
		"force_shell_command":         true,
	}
	for key := range config {
		if !allowed[key] {
			return nil, connectortargets.ValidationError("unsupported SSH connector field " + key)
		}
	}
	return map[string]any{
		"host":                        config["host"],
		"port":                        config["port"],
		"description":                 config["description"],
		"startup_input_after_connect": config["startup_input_after_connect"],
		"force_shell_command":         config["force_shell_command"],
	}, nil
}

func runtimeIDForTargetRef(ctx context.Context, runtime connectorapi.GatewayRuntime, targetRef string) (int64, error) {
	database, err := databaseFrom(runtime)
	if err != nil {
		return 0, err
	}
	targetID, profileID, ok := connectortargets.ParseTargetProfileRef(sshconnector.Kind, targetRef)
	if !ok {
		return 0, connectortargets.ErrInvalidTargetRef
	}
	store := connectortargets.NewStore(database)
	target, profile, err := store.ResolveConnectorActionTarget(ctx, targetRef)
	if err != nil {
		return 0, err
	}
	surface, err := store.EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind:  sshconnector.Kind,
		TargetID:       targetID,
		ProfileID:      profileID,
		CapabilityKind: connectortargets.RuntimeCapabilityLiveConsole,
		Label:          profile.Label,
	})
	if err != nil {
		return 0, err
	}
	if surface.TargetID != target.ID || surface.ProfileID != profile.ID {
		return 0, connectortargets.ErrRuntimeSurfaceNotFound
	}
	return surface.ID, nil
}

func ensureLiveConsoleRuntimeIDForProfile(ctx context.Context, runtime connectorapi.GatewayRuntime, targetID int64, profileID int64, label string) (int64, error) {
	database, err := databaseFrom(runtime)
	if err != nil {
		return 0, err
	}
	surface, err := connectortargets.NewStore(database).EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind:  sshconnector.Kind,
		TargetID:       targetID,
		ProfileID:      profileID,
		CapabilityKind: connectortargets.RuntimeCapabilityLiveConsole,
		Label:          label,
	})
	if err != nil {
		return 0, err
	}
	return surface.ID, nil
}

func existingLiveConsoleRuntimeIDsForProfile(ctx context.Context, runtime connectorapi.GatewayRuntime, targetID int64, profileID int64) ([]int64, error) {
	database, err := databaseFrom(runtime)
	if err != nil {
		return nil, err
	}
	surfaces, err := connectortargets.NewStore(database).ListRuntimeSurfacesForProfile(ctx, targetID, profileID, connectortargets.RuntimeCapabilityLiveConsole)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(surfaces))
	for _, surface := range surfaces {
		if surface.ConnectorKind == sshconnector.Kind {
			ids = append(ids, surface.ID)
		}
	}
	return ids, nil
}

func targetMaterial(ctx context.Context, runtime connectorapi.GatewayRuntime, runtimeID int64) (sshTargetMaterial, sshkeys.PrivateKey, error) {
	database, err := databaseFrom(runtime)
	if err != nil {
		return sshTargetMaterial{}, sshkeys.PrivateKey{}, err
	}
	target, profile, surface, err := connectortargets.NewStore(database).TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return sshTargetMaterial{}, sshkeys.PrivateKey{}, err
	}
	if surface.ConnectorKind != sshconnector.Kind ||
		(surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole && surface.CapabilityKind != connectortargets.RuntimeCapabilityFileTransfer) {
		return sshTargetMaterial{}, sshkeys.PrivateKey{}, connectortargets.ErrRuntimeSurfaceNotFound
	}
	host := strings.TrimSpace(stringConfigValue(target.Config, "host"))
	port := intConfigValue(target.Config, "port", 22)
	username := strings.TrimSpace(stringConfigValue(profile.Public, "username"))
	keyID := int64ConfigValue(profile.Public, "ssh_key_id")
	if host == "" || username == "" || keyID < 1 {
		return sshTargetMaterial{}, sshkeys.PrivateKey{}, errors.New("ssh connector profile is missing host, username, or key")
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		return sshTargetMaterial{}, sshkeys.PrivateKey{}, err
	}
	privateKey, err := keyStore.GetPrivateKey(ctx, keyID)
	if err != nil {
		return sshTargetMaterial{}, sshkeys.PrivateKey{}, err
	}
	return sshTargetMaterial{
		ID:                       runtimeID,
		Name:                     target.Name,
		Host:                     host,
		Port:                     port,
		Username:                 username,
		StartupInputAfterConnect: strings.TrimSpace(stringConfigValue(target.Config, "startup_input_after_connect")),
		ForceShellCommand:        strings.TrimSpace(stringConfigValue(target.Config, "force_shell_command")),
	}, privateKey, nil
}

func databaseFrom(runtime connectorapi.GatewayRuntime) (*sql.DB, error) {
	if runtime == nil || runtime.ConnectorDatabase() == nil {
		return nil, fmt.Errorf("database runtime is not available")
	}
	return runtime.ConnectorDatabase(), nil
}

func keyStore(runtime connectorapi.GatewayRuntime) (*sshkeys.Store, error) {
	if runtime == nil {
		return nil, fmt.Errorf("ssh key store is not available")
	}
	store, ok := runtime.ConnectorResource(sshconnector.Kind, "keys").(*sshkeys.Store)
	if !ok || store == nil {
		return nil, fmt.Errorf("ssh key store is not available")
	}
	return store, nil
}

func consoleSessions(runtime connectorapi.GatewayRuntime) (*console.Manager, error) {
	if runtime == nil || runtime.ConnectorConsoleSessions() == nil {
		return nil, fmt.Errorf("ssh console runtime is not available")
	}
	return runtime.ConnectorConsoleSessions(), nil
}

func executionTarget(gateway connectorapi.GatewayServer, target sshTargetMaterial, privateKey sshkeys.PrivateKey) execution.Target {
	return execution.Target{
		Host:           target.Host,
		Port:           target.Port,
		Username:       target.Username,
		PrivateKey:     privateKey.PrivateKey,
		KnownHostsPath: gateway.ConnectorTrustStorePath(),
	}
}

func executionTransferOptions(options connectorapi.TransferOptions) execution.TransferOptions {
	return execution.TransferOptions{
		Progress: func(transferred int64, total int64) {
			if options.Progress != nil {
				options.Progress(transferred, total)
			}
		},
		Wait: options.Wait,
	}
}

func connectorTransferResult(result execution.TransferResult) connectorapi.TransferResult {
	return connectorapi.TransferResult{
		Bytes:          result.Bytes,
		Size:           result.Size,
		ChecksumSHA256: result.ChecksumSHA256,
		DurationMS:     result.DurationMS,
	}
}

func remoteFileEntries(entries []execution.RemoteFileEntry) []connectorapi.RemoteFileEntry {
	items := make([]connectorapi.RemoteFileEntry, 0, len(entries))
	for _, entry := range entries {
		items = append(items, connectorapi.RemoteFileEntry{
			Name:       entry.Name,
			Path:       entry.Path,
			Type:       entry.Type,
			Size:       entry.Size,
			ModifiedAt: entry.ModifiedAt,
		})
	}
	return items
}

func serverFrom(value connectorapi.GatewayServer) (connectorapi.GatewayServer, error) {
	if value == nil {
		return nil, fmt.Errorf("gateway services are not available")
	}
	return value, nil
}

func serverFromHandler(value connectorapi.TargetLifecycleGateway) (connectorapi.GatewayServer, error) {
	if value == nil {
		return nil, fmt.Errorf("gateway handler services are not available")
	}
	return serverFrom(value.ConnectorServer())
}
