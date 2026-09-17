package management

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func (Management) BeforeCreateCredentialProfile(context.Context, connectorapi.TargetLifecycleRuntime, connectorapi.Target) error {
	return nil
}

func (Management) BeforeDeleteCredentialProfile(ctx context.Context, handler connectorapi.ConsoleRestartGateway, runtime connectorapi.TargetLifecycleRuntime, _ connectorapi.Target, profile connectorapi.CredentialProfile) error {
	gateway, err := consoleRestartGatewayFrom(handler)
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
		if _, err := gateway.ConnectorRestartConsoleSession(ctx, principal, runtimeID, "SSH credential profile was deleted before command completed"); err != nil {
			return err
		}
	}
	return nil
}

func (Management) DeleteTarget(handler connectorapi.TargetDeletionGateway, w http.ResponseWriter, r *http.Request, runtime connectorapi.TargetLifecycleRuntime, target connectorapi.Target) {
	if w == nil || r == nil {
		return
	}
	gateway, err := targetDeletionGatewayFrom(handler)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	profiles, err := runtime.ListCredentialProfiles(r.Context(), target.ID)
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
			remoteTarget, privateKey, err := TargetMaterialForRuntime(r.Context(), runtime, runtimeID)
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
			result, err := execution.RunCommand(ctx, ExecutionTarget(gateway, remoteTarget, privateKey), removeAuthorizedKeyCommand(key.PublicKey))
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
			result, err := gateway.ConnectorRestartConsoleSession(r.Context(), principal, runtimeID, "SSH connector target was deleted before command completed")
			if err != nil {
				writeInternalError(w)
				return
			}
			canceledCommands += result.CanceledRunningRequests
		}
	}
	if err := handler.ConnectorDeleteTargetRecord(r.Context(), target, map[string]any{
		"remote_key_removed":  removedKeys > 0,
		"remote_keys_removed": removedKeys,
		"canceled_commands":   canceledCommands,
	}); err != nil {
		handleTargetError(w, err)
		return
	}
	if _, err := handler.ConnectorFinalizeDeletedTarget(r.Context(), target, "SSH connector target was deleted; ask the AI to send a fresh request", nil); err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "remote_key_removed": removedKeys > 0, "remote_keys_removed": removedKeys})
}

func (Management) TestCredentialProfile(ctx context.Context, handler connectorapi.PeerIdentityGateway, runtime connectorapi.ConnectorDataRuntime, target connectors.TargetView, profile connectors.CredentialProfileView) (connectors.ManagementResponse, error) {
	gateway, err := PeerIdentityFrom(handler)
	if err != nil {
		return connectors.ManagementResponse{}, err
	}
	const command = `printf 'aipermission-ok\n'; uname -a`
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	start := time.Now()
	runtimeID, err := ensureLiveConsoleRuntimeIDForProfile(ctx, runtime, target.ID, profile.ID, profile.Label)
	if err != nil {
		return targetErrorResponse(err)
	}
	remoteTarget, privateKey, err := TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return materialErrorResponse(err)
	}
	result, err := execution.RunCommand(ctx, ExecutionTarget(gateway, remoteTarget, privateKey), command)
	if err != nil {
		if response, ok := managementUnknownHostKeyResponse(err, privateKey.PrivateKey); ok {
			return response, nil
		}
		return managementResponse(http.StatusOK, targetTestResponse{
			TargetID:      target.ID,
			ProfileID:     profile.ID,
			ConnectorKind: target.ConnectorKind,
			OK:            false,
			Status:        "connection_failed",
			Message:       ConnectionFailureMessage(err),
			DurationMS:    time.Since(start).Milliseconds(),
		}, privateKey.PrivateKey), nil
	}
	return managementResponse(http.StatusOK, targetTestResponse{
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
	}, privateKey.PrivateKey), nil
}

func (Management) TestDraft(ctx context.Context, handler connectorapi.PeerIdentityGateway, runtime connectorapi.ConnectorDataRuntime, requestValue any) (connectors.ManagementResponse, error) {
	gateway, err := PeerIdentityFrom(handler)
	if err != nil {
		return connectors.ManagementResponse{}, err
	}
	draft, err := decodeDraftRequest(requestValue)
	if err != nil {
		return managementErrorResponse(http.StatusBadRequest, err.Error()), nil
	}
	payload, err := connectorPayload(ctx, runtime, draft.Name, draft.Config, draft.Profile)
	if err != nil {
		return targetErrorResponse(err)
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		return connectors.ManagementResponse{}, err
	}
	privateKey, err := keyStore.GetPrivateKey(ctx, int64ConfigValue(payload.ProfilePublic, "ssh_key_id"))
	if err != nil {
		return keyErrorResponse(err)
	}
	const command = `printf 'aipermission-ok\n'; uname -a`
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
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
		if response, ok := managementUnknownHostKeyResponse(err, privateKey.PrivateKey); ok {
			return response, nil
		}
		return managementResponse(http.StatusOK, targetTestResponse{
			ConnectorKind: sshconnector.Kind,
			OK:            false,
			Status:        "connection_failed",
			Message:       ConnectionFailureMessage(err),
			DurationMS:    time.Since(start).Milliseconds(),
		}, privateKey.PrivateKey), nil
	}
	return managementResponse(http.StatusOK, targetTestResponse{
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
	}, privateKey.PrivateKey), nil
}

func (Management) RunTargetOperation(ctx context.Context, handler connectorapi.TargetOperationGateway, runtime connectorapi.ConnectorDataRuntime, target connectorapi.Target, operation string, requestValue any) (connectors.ManagementResponse, error) {
	gateway, err := targetOperationGatewayFrom(handler)
	if err != nil {
		return connectors.ManagementResponse{}, err
	}
	input, err := decodeTargetOperationRequest(requestValue)
	if err != nil {
		return managementErrorResponse(http.StatusBadRequest, "invalid json body"), nil
	}
	profiles, err := runtime.ListCredentialProfiles(ctx, target.ID)
	if err != nil {
		return targetErrorResponse(err)
	}
	profileID, err := operationProfileID(profiles, input.ProfileID)
	if err != nil {
		return targetErrorResponse(err)
	}
	targetRef := connectors.FormatTargetRef(sshconnector.Kind, target.ID, profileID)
	runtimeID, err := RuntimeIDForTargetRef(ctx, runtime, targetRef)
	if err != nil {
		return targetErrorResponse(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	remoteTarget, privateKey, err := TargetMaterialForRuntime(ctx, runtime, runtimeID)
	if err != nil {
		return materialErrorResponse(err)
	}
	switch operation {
	case "docker-check":
		response, err := dockerCheckForTarget(ctx, gateway, remoteTarget, privateKey)
		if err != nil {
			if projected, ok := managementUnknownHostKeyResponse(err, privateKey.PrivateKey); ok {
				return projected, nil
			}
			return sensitiveManagementErrorResponse(http.StatusBadGateway, CommandFailureMessage(err), privateKey.PrivateKey), nil
		}
		handler.ConnectorWriteAudit(ctx, "user", nil, remoteTarget.ID, "server.docker_check", map[string]any{
			"available":  response.Available,
			"exit_code":  response.ExitCode,
			"containers": len(response.Containers),
		})
		return managementResponse(http.StatusOK, response, privateKey.PrivateKey), nil
	case "docker-logs":
		containerRef := strings.TrimSpace(input.ContainerRef)
		if err := validateDockerContainerRef(containerRef); err != nil {
			return managementErrorResponse(http.StatusBadRequest, err.Error()), nil
		}
		response, err := dockerLogsForTarget(ctx, gateway, remoteTarget, privateKey, containerRef, input.Tail)
		if err != nil {
			if projected, ok := managementUnknownHostKeyResponse(err, privateKey.PrivateKey); ok {
				return projected, nil
			}
			return sensitiveManagementErrorResponse(http.StatusBadGateway, CommandFailureMessage(err), privateKey.PrivateKey), nil
		}
		handler.ConnectorWriteAudit(ctx, "user", nil, remoteTarget.ID, "server.docker_logs", map[string]any{
			"container_ref": containerRef,
			"exit_code":     response.ExitCode,
			"tail":          normalizeDockerLogsTail(input.Tail),
		})
		return managementResponse(http.StatusOK, response, privateKey.PrivateKey), nil
	default:
		return managementErrorResponse(http.StatusBadRequest, "unsupported connector operation"), nil
	}
}

func (Management) CanonicalCredentialPublic(ctx context.Context, runtime connectorapi.ConnectorDataRuntime, credentialKind string, public map[string]any) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return canonicalCredentialPublic(ctx, runtime, credentialKind, public)
}

func dockerCheckForTarget(ctx context.Context, gateway connectorapi.PeerIdentityGateway, target TargetMaterial, privateKey sshkeys.PrivateKey) (dockerCheckResponse, error) {
	const command = `if ! command -v docker >/dev/null 2>&1; then
  printf '__AIPERMISSION_DOCKER_UNAVAILABLE__\n'
  exit 0
fi
docker ps --format '{{json .}}'`
	result, err := execution.RunCommand(ctx, ExecutionTarget(gateway, target, privateKey), command)
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

func dockerLogsForTarget(ctx context.Context, gateway connectorapi.PeerIdentityGateway, target TargetMaterial, privateKey sshkeys.PrivateKey, containerRef string, tailValue int) (dockerLogsResponse, error) {
	tail := normalizeDockerLogsTail(tailValue)
	command := fmt.Sprintf(`if ! command -v docker >/dev/null 2>&1; then
  printf 'docker command is not available\n' >&2
  exit 127
fi
docker logs --tail %s --timestamps %s`, strconv.Itoa(tail), shellQuote(containerRef))
	result, err := execution.RunCommand(ctx, ExecutionTarget(gateway, target, privateKey), command)
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
