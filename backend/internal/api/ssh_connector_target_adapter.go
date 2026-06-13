package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/execution"
	"github.com/aipermission/aipermission/backend/internal/sshkeys"
)

func (sshRuntimeAdapter) BeforeCreateCredentialProfile(context.Context, *databaseRuntime, *connectortargets.Store, connectortargets.Target) error {
	return nil
}

func (sshRuntimeAdapter) BeforeDeleteCredentialProfile(ctx context.Context, handler connectorTargetHandlers, runtime *databaseRuntime, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
	_, err := handler.restartServerConsoleSession(ctx, runtime, profile.ID, "SSH credential profile was deleted before command completed")
	return err
}

func (sshRuntimeAdapter) DeleteTarget(handler connectorTargetHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, target connectortargets.Target) {
	store := connectortargets.NewStore(runtime.database)
	profiles, err := store.ListCredentialProfiles(r.Context(), target.ID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	removedKeys := int64(0)
	if r.URL.Query().Get("remove_key") == "true" {
		if len(profiles) == 0 {
			writeError(w, http.StatusBadRequest, "remote SSH key cleanup requires a saved credential profile")
			return
		}
		cleanupSeen := map[string]bool{}
		for _, profile := range profiles {
			server, privateKey, err := handler.serverSSHMaterial(r.Context(), profile.ID)
			if err != nil {
				handleServerSSHMaterialError(w, err)
				return
			}
			sshKeyID := int64ConfigValue(profile.Public, "ssh_key_id")
			sshKey, err := runtime.sshKeys.Get(r.Context(), sshKeyID)
			if err != nil {
				writeInternalError(w)
				return
			}
			cleanupKey := server.Username + "\x00" + publicKeyBlob(sshKey.PublicKey)
			if cleanupSeen[cleanupKey] {
				continue
			}
			cleanupSeen[cleanupKey] = true
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			result, err := execution.RunCommand(ctx, handler.executionTarget(server, privateKey), removeAuthorizedKeyCommand(sshKey.PublicKey))
			cancel()
			if err != nil {
				writeError(w, http.StatusBadGateway, "remote key uninstall failed")
				return
			}
			if result.ExitCode != 0 {
				message := strings.TrimSpace(result.Stderr + result.Stdout)
				if message == "" {
					message = "remote key uninstall failed"
				}
				if remoteKeyAlreadyAbsent(message) {
					continue
				}
				writeError(w, http.StatusBadGateway, message)
				return
			}
			removedKeys++
		}
	}
	staleRequests, err := handler.staleConnectorActionRequestsForTarget(r.Context(), runtime, target.ID, 0, "SSH connector target was deleted; ask the AI to send a fresh request")
	if err != nil {
		writeInternalError(w)
		return
	}
	canceledCommands := int64(0)
	for _, profile := range profiles {
		result, err := handler.restartServerConsoleSession(r.Context(), runtime, profile.ID, "SSH connector target was deleted before command completed")
		if err != nil {
			writeInternalError(w)
			return
		}
		canceledCommands += result.CanceledRunningRequests
	}
	if err := store.DeleteTarget(r.Context(), target.ID); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	handler.writeAudit(r.Context(), runtime, "user", nil, 0, "connector.target.deleted", map[string]any{
		"target_id":                target.ID,
		"connector_kind":           target.ConnectorKind,
		"name":                     target.Name,
		"remote_key_removed":       removedKeys > 0,
		"remote_keys_removed":      removedKeys,
		"stale_connector_requests": staleRequests,
		"canceled_commands":        canceledCommands,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "remote_key_removed": removedKeys > 0, "remote_keys_removed": removedKeys})
}

func remoteKeyAlreadyAbsent(message string) bool {
	return strings.Contains(message, "remote key uninstall removed 0 authorized_keys entries")
}

func (sshRuntimeAdapter) TestCredentialProfile(handler connectorTargetHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, target connectors.TargetView, profile connectors.CredentialProfileView) {
	const command = `printf 'aipermission-ok\n'; uname -a`
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	server, privateKey, err := handler.serverSSHMaterial(ctx, profile.ID)
	if err != nil {
		handleServerSSHMaterialError(w, err)
		return
	}
	result, err := execution.RunCommand(ctx, handler.executionTarget(server, privateKey), command)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, connectorTargetTestResponse{
			TargetID:      target.ID,
			ProfileID:     profile.ID,
			ConnectorKind: target.ConnectorKind,
			OK:            false,
			Status:        "connection_failed",
			Message:       sshConnectionFailureMessage(err),
			DurationMS:    time.Since(start).Milliseconds(),
		})
		return
	}
	writeJSON(w, http.StatusOK, connectorTargetTestResponse{
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: target.ConnectorKind,
		OK:            result.ExitCode == 0,
		Status:        "ok",
		Message:       strings.TrimSpace(result.Stderr + result.Stdout),
		Details: map[string]any{
			"command":   command,
			"stdout":    result.Stdout,
			"stderr":    result.Stderr,
			"exit_code": result.ExitCode,
		},
		DurationMS: result.DurationMS,
	})
}

func (sshRuntimeAdapter) TestDraft(handler connectorTargetHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, request createConnectorTargetRequest) {
	payload, err := handler.sshConnectorPayload(r.Context(), runtime, request.Name, request.Config)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	privateKey, err := runtime.sshKeys.GetPrivateKey(r.Context(), int64ConfigValue(payload.ProfilePublic, "ssh_key_id"))
	if err != nil {
		handleSSHKeyError(w, err)
		return
	}
	const command = `printf 'aipermission-ok\n'; uname -a`
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	result, err := execution.RunCommand(ctx, execution.Target{
		Host:           stringConfigValue(payload.TargetConfig, "host"),
		Port:           intConfigValue(payload.TargetConfig, "port", 22),
		Username:       stringConfigValue(payload.ProfilePublic, "username"),
		PrivateKey:     privateKey.PrivateKey,
		KnownHostsPath: handler.knownHostsPath(),
	}, command)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, connectorTargetTestResponse{
			ConnectorKind: sshconnector.Kind,
			OK:            false,
			Status:        "connection_failed",
			Message:       sshConnectionFailureMessage(err),
			DurationMS:    time.Since(start).Milliseconds(),
		})
		return
	}
	writeJSON(w, http.StatusOK, connectorTargetTestResponse{
		ConnectorKind: sshconnector.Kind,
		OK:            result.ExitCode == 0,
		Status:        "ok",
		Message:       strings.TrimSpace(result.Stderr + result.Stdout),
		Details: map[string]any{
			"command":   command,
			"stdout":    result.Stdout,
			"stderr":    result.Stderr,
			"exit_code": result.ExitCode,
		},
		DurationMS: result.DurationMS,
	})
}

func (sshRuntimeAdapter) RunTargetOperation(handler connectorTargetHandlers, w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, target connectortargets.Target, operation string) {
	var request connectorTargetOperationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	store := connectortargets.NewStore(runtime.database)
	profileID, err := sshOperationProfileID(r.Context(), store, target.ID, request.ProfileID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	mapping, _, _, err := store.SSHRuntimeForTargetRef(r.Context(), connectortargets.SSHTargetRef(target.ID, profileID))
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	server, privateKey, err := handler.serverSSHMaterial(ctx, mapping.ServerID)
	if err != nil {
		handleServerSSHMaterialError(w, err)
		return
	}
	switch operation {
	case "docker-check":
		response, err := handler.dockerCheckForServer(ctx, runtime, server, privateKey)
		if err != nil {
			if writeUnknownHostKeyError(w, err) {
				return
			}
			writeError(w, http.StatusBadGateway, sshCommandFailureMessage(err))
			return
		}
		writeJSON(w, http.StatusOK, response)
	case "docker-logs":
		containerRef := strings.TrimSpace(request.ContainerRef)
		if err := validateDockerContainerRef(containerRef); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := handler.dockerLogsForServer(ctx, runtime, server, privateKey, containerRef, request.Tail)
		if err != nil {
			if writeUnknownHostKeyError(w, err) {
				return
			}
			writeError(w, http.StatusBadGateway, sshCommandFailureMessage(err))
			return
		}
		writeJSON(w, http.StatusOK, response)
	default:
		writeError(w, http.StatusBadRequest, "unsupported connector operation")
	}
}

func (sshRuntimeAdapter) CanonicalCredentialPublic(ctx context.Context, handler connectorTargetHandlers, runtime *databaseRuntime, credentialKind string, public map[string]any) (map[string]any, error) {
	if strings.TrimSpace(credentialKind) != "private_key" {
		return nil, connectortargets.ValidationError("unsupported SSH credential kind")
	}
	return handler.canonicalSSHCredentialPublic(ctx, runtime, public)
}

func (sshRuntimeAdapter) LiveConsoleProfileID(profileID int64) int64 {
	return profileID
}

func (sshRuntimeAdapter) LiveConsoleTargetMetadata(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
	metadata := map[string]any{}
	if host := stringConfigValue(target.Config, "host"); host != "" {
		metadata["host"] = host
	}
	if port := intConfigValue(target.Config, "port", 22); port > 0 {
		metadata["port"] = port
	}
	if username := stringConfigValue(profile.Public, "username"); username != "" {
		metadata["username"] = username
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func sshOperationProfileID(ctx context.Context, store *connectortargets.Store, targetID int64, requestedProfileID int64) (int64, error) {
	if requestedProfileID > 0 {
		return requestedProfileID, nil
	}
	profiles, err := store.ListCredentialProfiles(ctx, targetID)
	if err != nil {
		return 0, err
	}
	if len(profiles) == 0 {
		return 0, connectortargets.ErrTargetProfileNotFound
	}
	if len(profiles) > 1 {
		return 0, connectortargets.ValidationError("profile_id is required when an SSH connector target has multiple credential profiles")
	}
	return profiles[0].ID, nil
}

type sshConnectorPayload struct {
	Name          string
	TargetConfig  map[string]any
	ProfileLabel  string
	ProfilePublic map[string]any
}

func (handler connectorTargetHandlers) sshConnectorPayload(ctx context.Context, runtime *databaseRuntime, name string, config map[string]any) (sshConnectorPayload, error) {
	if config == nil {
		config = map[string]any{}
	}
	targetConfig, err := sshTargetConfigFromConnectorConfig(config)
	if err != nil {
		return sshConnectorPayload{}, err
	}
	sshConnector := sshconnector.New()
	if err := connectors.ValidateNonSecretSchema(sshConnector.TargetSchema(), "ssh target"); err != nil {
		return sshConnectorPayload{}, err
	}
	if err := connectors.ValidateSchemaValues(sshConnector.TargetSchema(), targetConfig); err != nil {
		return sshConnectorPayload{}, err
	}
	username := stringConfigValue(config, "username")
	if username == "" {
		return sshConnectorPayload{}, connectortargets.ValidationError("username is required")
	}
	profilePublic, err := handler.canonicalSSHCredentialPublic(ctx, runtime, map[string]any{
		"username":   username,
		"ssh_key_id": config["ssh_key_id"],
	})
	if err != nil {
		return sshConnectorPayload{}, err
	}
	return sshConnectorPayload{
		Name:          strings.TrimSpace(name),
		TargetConfig:  targetConfig,
		ProfileLabel:  strings.TrimSpace(username),
		ProfilePublic: profilePublic,
	}, nil
}

func (handler connectorTargetHandlers) canonicalSSHCredentialPublic(ctx context.Context, runtime *databaseRuntime, public map[string]any) (map[string]any, error) {
	username := stringConfigValue(public, "username")
	if username == "" {
		return nil, connectortargets.ValidationError("username is required")
	}
	sshKeyID := int64ConfigValue(public, "ssh_key_id")
	if sshKeyID < 1 {
		return nil, connectortargets.ValidationError("ssh_key_id is required")
	}
	key, err := runtime.sshKeys.Get(ctx, sshKeyID)
	if err != nil {
		if errors.Is(err, sshkeys.ErrNotFound) {
			return nil, connectortargets.ValidationError("ssh_key_id does not reference an existing SSH credential")
		}
		return nil, err
	}
	return map[string]any{
		"username":    username,
		"ssh_key_id":  key.ID,
		"key_name":    key.Name,
		"key_type":    key.KeyType,
		"fingerprint": key.Fingerprint,
	}, nil
}

func sshTargetConfigFromConnectorConfig(config map[string]any) (map[string]any, error) {
	allowed := map[string]bool{
		"host":                        true,
		"port":                        true,
		"description":                 true,
		"startup_input_after_connect": true,
		"force_shell_command":         true,
		"username":                    true,
		"ssh_key_id":                  true,
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
