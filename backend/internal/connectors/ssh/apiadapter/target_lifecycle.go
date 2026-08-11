package apiadapter

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (adapter) BeforeCreateCredentialProfile(context.Context, connectorapi.GatewayRuntime, *connectortargets.Store, connectortargets.Target) error {
	return nil
}

func (adapter) BeforeDeleteCredentialProfile(ctx context.Context, handler connectorapi.TargetLifecycleGateway, runtime connectorapi.GatewayRuntime, _ *connectortargets.Store, _ connectortargets.Target, profile connectortargets.CredentialProfile) error {
	gateway, err := serverFromHandler(handler)
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runtimeIDs, err := existingLiveConsoleRuntimeIDsForProfile(ctx, runtime, profile.TargetID, profile.ID)
	if err != nil {
		return err
	}
	principal, err := runtime.ConnectorLocalExecutionPrincipal()
	if err != nil {
		return err
	}
	for _, runtimeID := range runtimeIDs {
		if _, err := gateway.ConnectorRestartConsoleSession(ctx, runtime, principal, runtimeID, "SSH credential profile was deleted before command completed"); err != nil {
			return err
		}
	}
	return nil
}

func (adapter) DeleteTarget(handler connectorapi.TargetLifecycleGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime, target connectortargets.Target) {
	if w == nil || r == nil {
		return
	}
	gateway, err := serverFromHandler(handler)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	database, err := databaseFrom(runtime)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	store := connectortargets.NewStore(database)
	profiles, err := store.ListCredentialProfiles(r.Context(), target.ID)
	if err != nil {
		handleTargetError(w, err)
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
			runtimeID, err := ensureLiveConsoleRuntimeIDForProfile(r.Context(), runtime, target.ID, profile.ID, profile.Label)
			if err != nil {
				handleTargetError(w, err)
				return
			}
			remoteTarget, privateKey, err := targetMaterial(r.Context(), runtime, runtimeID)
			if err != nil {
				handleMaterialError(w, err)
				return
			}
			keyStore, err := keyStore(runtime)
			if err != nil {
				writeInternalError(w)
				return
			}
			sshKeyID := int64ConfigValue(profile.Public, "ssh_key_id")
			key, err := keyStore.Get(r.Context(), sshKeyID)
			if err != nil {
				handleKeyError(w, err)
				return
			}
			cleanupKey := remoteTarget.Username + "\x00" + publicKeyBlob(key.PublicKey)
			if cleanupSeen[cleanupKey] {
				continue
			}
			cleanupSeen[cleanupKey] = true
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			result, err := execution.RunCommand(ctx, executionTarget(gateway, remoteTarget, privateKey), removeAuthorizedKeyCommand(key.PublicKey))
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
	canceledCommands := int64(0)
	principal, err := runtime.ConnectorLocalExecutionPrincipal()
	if err != nil {
		writeInternalError(w)
		return
	}
	for _, profile := range profiles {
		runtimeIDs, err := existingLiveConsoleRuntimeIDsForProfile(r.Context(), runtime, target.ID, profile.ID)
		if err != nil {
			writeInternalError(w)
			return
		}
		for _, runtimeID := range runtimeIDs {
			result, err := gateway.ConnectorRestartConsoleSession(r.Context(), runtime, principal, runtimeID, "SSH connector target was deleted before command completed")
			if err != nil {
				writeInternalError(w)
				return
			}
			canceledCommands += result.CanceledRunningRequests
		}
	}
	if err := handler.ConnectorDeleteTargetRecord(r.Context(), runtime, target, map[string]any{
		"remote_key_removed":  removedKeys > 0,
		"remote_keys_removed": removedKeys,
		"canceled_commands":   canceledCommands,
	}); err != nil {
		handleTargetError(w, err)
		return
	}
	if _, err := handler.ConnectorFinalizeDeletedTarget(r.Context(), runtime, target, "SSH connector target was deleted; ask the AI to send a fresh request", nil); err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "remote_key_removed": removedKeys > 0, "remote_keys_removed": removedKeys})
}

func (adapter) TestCredentialProfile(handler connectorapi.TargetLifecycleGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime, target connectors.TargetView, profile connectors.CredentialProfileView) {
	if w == nil || r == nil {
		return
	}
	gateway, err := serverFromHandler(handler)
	if err != nil {
		writeInternalError(w)
		return
	}
	const command = `printf 'aipermission-ok\n'; uname -a`
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	runtimeID, err := ensureLiveConsoleRuntimeIDForProfile(ctx, runtime, target.ID, profile.ID, profile.Label)
	if err != nil {
		handleTargetError(w, err)
		return
	}
	remoteTarget, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		handleMaterialError(w, err)
		return
	}
	result, err := execution.RunCommand(ctx, executionTarget(gateway, remoteTarget, privateKey), command)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, targetTestResponse{
			TargetID:      target.ID,
			ProfileID:     profile.ID,
			ConnectorKind: target.ConnectorKind,
			OK:            false,
			Status:        "connection_failed",
			Message:       connectionFailureMessage(err),
			DurationMS:    time.Since(start).Milliseconds(),
		})
		return
	}
	writeJSON(w, http.StatusOK, targetTestResponse{
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

func (adapter) TestDraft(handler connectorapi.TargetLifecycleGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime, requestValue any) {
	if w == nil || r == nil {
		return
	}
	gateway, err := serverFromHandler(handler)
	if err != nil {
		writeInternalError(w)
		return
	}
	draft, err := decodeDraftRequest(requestValue)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload, err := connectorPayload(r.Context(), runtime, draft.Name, draft.Config, draft.Profile)
	if err != nil {
		handleTargetError(w, err)
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	privateKey, err := keyStore.GetPrivateKey(r.Context(), int64ConfigValue(payload.ProfilePublic, "ssh_key_id"))
	if err != nil {
		handleKeyError(w, err)
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
		KnownHostsPath: gateway.ConnectorTrustStorePath(),
	}, command)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, targetTestResponse{
			ConnectorKind: sshconnector.Kind,
			OK:            false,
			Status:        "connection_failed",
			Message:       connectionFailureMessage(err),
			DurationMS:    time.Since(start).Milliseconds(),
		})
		return
	}
	writeJSON(w, http.StatusOK, targetTestResponse{
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

func (adapter) RunTargetOperation(handler connectorapi.TargetLifecycleGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.GatewayRuntime, target connectortargets.Target, operation string) {
	if w == nil || r == nil {
		return
	}
	gateway, err := serverFromHandler(handler)
	if err != nil {
		writeInternalError(w)
		return
	}
	var input targetOperationRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	database, err := databaseFrom(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	store := connectortargets.NewStore(database)
	profileID, err := operationProfileID(r.Context(), store, target.ID, input.ProfileID)
	if err != nil {
		handleTargetError(w, err)
		return
	}
	targetRef := connectortargets.TargetProfileRef(sshconnector.Kind, target.ID, profileID)
	runtimeID, err := runtimeIDForTargetRef(r.Context(), runtime, targetRef)
	if err != nil {
		handleTargetError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	remoteTarget, privateKey, err := targetMaterial(ctx, runtime, runtimeID)
	if err != nil {
		handleMaterialError(w, err)
		return
	}
	switch operation {
	case "docker-check":
		response, err := dockerCheckForTarget(ctx, gateway, remoteTarget, privateKey)
		if err != nil {
			if writeUnknownHostKeyError(w, err) {
				return
			}
			writeError(w, http.StatusBadGateway, commandFailureMessage(err))
			return
		}
		handler.ConnectorWriteAudit(r.Context(), runtime, "user", nil, remoteTarget.ID, "server.docker_check", map[string]any{
			"available":  response.Available,
			"exit_code":  response.ExitCode,
			"containers": len(response.Containers),
		})
		writeJSON(w, http.StatusOK, response)
	case "docker-logs":
		containerRef := strings.TrimSpace(input.ContainerRef)
		if err := validateDockerContainerRef(containerRef); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := dockerLogsForTarget(ctx, gateway, remoteTarget, privateKey, containerRef, input.Tail)
		if err != nil {
			if writeUnknownHostKeyError(w, err) {
				return
			}
			writeError(w, http.StatusBadGateway, commandFailureMessage(err))
			return
		}
		handler.ConnectorWriteAudit(r.Context(), runtime, "user", nil, remoteTarget.ID, "server.docker_logs", map[string]any{
			"container_ref": containerRef,
			"exit_code":     response.ExitCode,
			"tail":          normalizeDockerLogsTail(input.Tail),
		})
		writeJSON(w, http.StatusOK, response)
	default:
		writeError(w, http.StatusBadRequest, "unsupported connector operation")
	}
}

func (adapter) CanonicalCredentialPublic(ctx context.Context, _ connectorapi.TargetLifecycleGateway, runtime connectorapi.GatewayRuntime, credentialKind string, public map[string]any) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return canonicalCredentialPublic(ctx, runtime, credentialKind, public)
}

func dockerCheckForTarget(ctx context.Context, gateway connectorapi.GatewayServer, target sshTargetMaterial, privateKey sshkeys.PrivateKey) (dockerCheckResponse, error) {
	const command = `if ! command -v docker >/dev/null 2>&1; then
  printf '__AIPERMISSION_DOCKER_UNAVAILABLE__\n'
  exit 0
fi
docker ps --format '{{json .}}'`
	result, err := execution.RunCommand(ctx, executionTarget(gateway, target, privateKey), command)
	if err != nil {
		return dockerCheckResponse{}, err
	}
	containers, available := parseDockerPSOutput(result.Stdout)
	return dockerCheckResponse{
		RuntimeID:  target.ID,
		TargetName: target.Name,
		Available:  available,
		OK:         available && result.ExitCode == 0,
		Command:    command,
		Containers: containers,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitCode:   result.ExitCode,
		DurationMS: result.DurationMS,
		CheckedAt:  time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func dockerLogsForTarget(ctx context.Context, gateway connectorapi.GatewayServer, target sshTargetMaterial, privateKey sshkeys.PrivateKey, containerRef string, tailValue int) (dockerLogsResponse, error) {
	tail := normalizeDockerLogsTail(tailValue)
	command := fmt.Sprintf(`if ! command -v docker >/dev/null 2>&1; then
  printf 'docker command is not available\n' >&2
  exit 127
fi
docker logs --tail %s --timestamps %s`, strconv.Itoa(tail), shellQuote(containerRef))
	result, err := execution.RunCommand(ctx, executionTarget(gateway, target, privateKey), command)
	if err != nil {
		return dockerLogsResponse{}, err
	}
	return dockerLogsResponse{
		RuntimeID:    target.ID,
		TargetName:   target.Name,
		ContainerRef: containerRef,
		OK:           result.ExitCode == 0,
		Command:      command,
		Stdout:       result.Stdout,
		Stderr:       result.Stderr,
		ExitCode:     result.ExitCode,
		DurationMS:   result.DurationMS,
		CheckedAt:    time.Now().UTC().Format(time.RFC3339),
	}, nil
}
