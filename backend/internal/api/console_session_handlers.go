package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

type createConsoleSessionRequest struct {
	RuntimeID     int64                           `json:"runtime_id"`
	Name          string                          `json:"name"`
	CloseExisting bool                            `json:"close_existing"`
	Cols          int                             `json:"cols"`
	Rows          int                             `json:"rows"`
	Params        map[string]any                  `json:"params,omitempty"`
	VaultItems    []projectvault.SessionSelection `json:"vault_items,omitempty"`
}

func (s consoleHandlers) listConsoleSessions(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	runtimeID := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("runtime_id")); raw != "" {
		parsed, ok := parseInt64Query(w, raw, "runtime_id")
		if !ok {
			return
		}
		runtimeID = parsed
	}
	items, err := runtime.consoleSessions.List(r.Context(), runtimeID)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s consoleHandlers) createConsoleSession(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var input createConsoleSessionRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	request := console.CreateRequest{
		RuntimeID: input.RuntimeID, Name: input.Name, CloseExisting: input.CloseExisting,
		Cols: input.Cols, Rows: input.Rows, Params: input.Params,
	}
	principal, err := localExecutionPrincipal(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	request.Principal = principal
	request.WaitForStart = true
	var snapshot vaultEnvironmentSnapshot
	if len(input.VaultItems) > 0 {
		vaultStore, err := projectvault.NewStore(runtime.database, runtime.vault, runtime.workspaceUUID)
		if err != nil {
			writeInternalError(w)
			return
		}
		snapshot, err = buildVaultEnvironmentSnapshot(r.Context(), s.Server, runtime, input.RuntimeID, input.VaultItems)
		if err != nil {
			handleVaultItemError(w, err)
			return
		}
		finalize := func(finalizeCtx context.Context, handle console.SessionHandle) error {
			if err := vaultStore.RecordSessionItems(finalizeCtx, handle.ID, snapshot.Items); err != nil {
				return err
			}
			return vaultStore.MarkSessionItemsUsed(finalizeCtx, snapshot.Items)
		}
		request.PrepareEnvironment = newVaultEnvironmentPreparer(
			s.Server, runtime, snapshot, input.VaultItems, nil, finalize,
		)
		request.EnvironmentContentHash = snapshot.EnvironmentContentHash
	}
	item, err := runtime.consoleSessions.Create(r.Context(), request)
	if errors.Is(err, console.ErrSessionLimit) {
		writeError(w, http.StatusConflict, err.Error())
		return
	} else if err != nil {
		adapter := s.consoleErrorPresenter(r.Context(), runtime, request.RuntimeID)
		if writeConnectorError(w, adapter, err) {
			return
		}
		writeError(w, http.StatusBadRequest, connectorErrorMessage(adapter, "console session failed", err))
		return
	}
	s.writeObservationAudit(r.Context(), runtime, "user", nil, item.RuntimeID, "console.session.created_observed", map[string]any{
		"session_id":               item.ID,
		"name":                     item.Name,
		"close_existing":           request.CloseExisting,
		"vault_item_ids":           vaultSessionItemIDs(snapshot.Items),
		"environment_content_hash": snapshot.EnvironmentContentHash,
	})
	writeJSON(w, http.StatusCreated, item)
}

func requireSessionEnvironmentCapability(ctx context.Context, server *Server, runtime *databaseRuntime, runtimeID int64) error {
	_, err := sessionEnvironmentCapabilityVersion(ctx, server, runtime, runtimeID)
	return err
}

func sessionEnvironmentCapabilityVersion(ctx context.Context, server *Server, runtime *databaseRuntime, runtimeID int64) (string, error) {
	sessionCapability, err := sessionEnvironmentCapabilityFor(ctx, server, runtime, runtimeID)
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(sessionCapability.SessionEnvironmentVersion())
	if version == "" {
		return "", errors.New("this connector runtime has an invalid Vault session environment version")
	}
	return version, nil
}

func sessionEnvironmentCapabilityFor(ctx context.Context, server *Server, runtime *databaseRuntime, runtimeID int64) (connectors.SessionEnvironmentCapability, error) {
	surface, err := connectortargets.NewStore(runtime.database).GetRuntimeSurface(ctx, runtimeID)
	if err != nil {
		return nil, err
	}
	capabilities := connectorRuntimeCapabilitiesFor(surface.ConnectorKind, server, runtime)
	if capabilities == nil {
		return nil, connectors.ErrSessionEnvironmentUnsupported
	}
	capability := capabilities.RuntimeCapability(connectors.SessionEnvironmentCapabilityName)
	sessionCapability, ok := capability.(connectors.SessionEnvironmentCapability)
	if !ok {
		return nil, connectors.ErrSessionEnvironmentUnsupported
	}
	return sessionCapability, nil
}

func vaultSessionItemIDs(items []projectvault.SessionItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ItemID)
	}
	return ids
}

func (s consoleHandlers) getConsoleSession(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	item, err := runtime.consoleSessions.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "console session not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s consoleHandlers) inputConsoleSession(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request console.InputRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	principal, err := localExecutionPrincipal(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := runtime.consoleSessions.Input(r.Context(), principal, id, request.Data); errors.Is(err, console.ErrInputTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	} else if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if runtimeID, err := consoleSessionRuntimeID(r.Context(), runtime, id); err == nil {
		s.writeObservationAudit(r.Context(), runtime, "user", nil, runtimeID, "console.session.input", map[string]any{
			"session_id": id,
			"bytes":      len(request.Data),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sent"})
}

func (s consoleHandlers) closeConsoleSession(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	principal, err := localExecutionPrincipal(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := runtime.consoleSessions.Close(r.Context(), principal, id); err != nil {
		writeInternalError(w)
		return
	}
	if err := s.cancelRunningCommandRequestsForSession(r.Context(), runtime, id, "console session closed before command completed"); err != nil {
		writeInternalError(w)
		return
	}
	if runtimeID, err := consoleSessionRuntimeID(r.Context(), runtime, id); err == nil {
		s.writeObservationAudit(r.Context(), runtime, "user", nil, runtimeID, "console.session.closed_observed", map[string]any{
			"session_id": id,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "closed"})
}

func (s consoleHandlers) restartTargetConsoleSession(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	runtimeID, ok := parseID(w, r)
	if !ok {
		return
	}
	principal, err := localExecutionPrincipal(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	result, err := s.Server.restartServerConsoleSession(r.Context(), runtime, principal, runtimeID, "console session restarted by local user before command completed")
	if err != nil {
		writeInternalError(w)
		return
	}
	s.writeObservationAudit(r.Context(), runtime, "user", nil, runtimeID, "console.session.restarted", map[string]any{
		"closed_session_ids":        result.ClosedSessionIDs,
		"canceled_running_requests": result.CanceledRunningRequests,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"status":                    "restarted",
		"runtime_id":                runtimeID,
		"target_id":                 runtimeID,
		"closed_session_ids":        result.ClosedSessionIDs,
		"canceled_running_requests": result.CanceledRunningRequests,
	})
}

func (s consoleHandlers) attachConsoleSession(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	principal, err := localExecutionPrincipal(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := runtime.consoleSessions.Attach(w, r, principal, id, s.upgradeWebSocket); errors.Is(err, console.ErrNotFound) {
		writeError(w, http.StatusNotFound, "console session not found")
	} else if errors.Is(err, console.ErrClientLimit) {
		writeError(w, http.StatusConflict, err.Error())
	} else if err != nil {
		var inactive console.InactiveError
		if errors.As(err, &inactive) {
			writeError(w, http.StatusConflict, inactive.Error())
			return
		}
		writeInternalError(w)
	}
}
