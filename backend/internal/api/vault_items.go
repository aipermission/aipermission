package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

type vaultUsageNoteRequest struct {
	Location string `json:"location"`
	Notes    string `json:"notes"`
}

type createVaultItemRequest struct {
	Name              string                  `json:"name"`
	Value             string                  `json:"value"`
	OwnerProjectID    int64                   `json:"owner_project_id"`
	SharedProjectIDs  []int64                 `json:"shared_project_ids"`
	SecretType        string                  `json:"secret_type"`
	Provider          string                  `json:"provider"`
	Environment       string                  `json:"environment"`
	Description       string                  `json:"description"`
	ExpiresAt         string                  `json:"expires_at"`
	ExpiryWarningDays int                     `json:"expiry_warning_days"`
	Source            string                  `json:"source"`
	GeneratorKind     string                  `json:"generator_kind"`
	Tags              []string                `json:"tags"`
	UsageNotes        []vaultUsageNoteRequest `json:"usage_notes"`
}

type updateVaultItemRequest struct {
	ExpectedMetadataRevision int64                   `json:"expected_metadata_revision"`
	Name                     string                  `json:"name"`
	OwnerProjectID           int64                   `json:"owner_project_id"`
	SharedProjectIDs         []int64                 `json:"shared_project_ids"`
	SecretType               string                  `json:"secret_type"`
	Provider                 string                  `json:"provider"`
	Environment              string                  `json:"environment"`
	Description              string                  `json:"description"`
	ExpiresAt                string                  `json:"expires_at"`
	ExpiryWarningDays        int                     `json:"expiry_warning_days"`
	Tags                     []string                `json:"tags"`
	UsageNotes               []vaultUsageNoteRequest `json:"usage_notes"`
}

type replaceVaultItemValueRequest struct {
	Value                string `json:"value"`
	ExpectedValueVersion int64  `json:"expected_value_version"`
}

type deleteVaultItemRequest struct {
	ExpectedValueVersion     int64 `json:"expected_value_version"`
	ExpectedMetadataRevision int64 `json:"expected_metadata_revision"`
}

func (s vaultItemHandlers) listVaultItems(w http.ResponseWriter, r *http.Request) {
	runtime, store, ok := s.store(w, r)
	if !ok {
		return
	}
	filter := projectvault.ListFilter{Query: strings.TrimSpace(r.URL.Query().Get("q"))}
	if raw := strings.TrimSpace(r.URL.Query().Get("project_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 1 {
			writeError(w, http.StatusBadRequest, "project_id must be a positive integer")
			return
		}
		filter.ProjectID = value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		filter.Limit = value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "offset must be an integer")
			return
		}
		filter.Offset = value
	}
	items, total, err := store.List(r.Context(), filter)
	if err != nil {
		writeInternalError(w)
		return
	}
	_ = runtime
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s vaultItemHandlers) createVaultItem(w http.ResponseWriter, r *http.Request) {
	runtime, store, ok := s.store(w, r)
	if !ok {
		return
	}
	var request createVaultItemRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.writeRequiredVaultAudit(r, runtime, "vault.item.create.requested", map[string]any{
		"project_id": request.OwnerProjectID,
		"name":       strings.TrimSpace(request.Name),
		"source":     request.Source,
	}); err != nil {
		writeInternalError(w)
		return
	}
	item, err := store.Create(r.Context(), projectvault.CreateInput{
		Name: request.Name, Value: request.Value, OwnerProjectID: request.OwnerProjectID,
		SharedProjectIDs: request.SharedProjectIDs, SecretType: request.SecretType,
		Provider: request.Provider, Environment: request.Environment, Description: request.Description,
		ExpiresAt: request.ExpiresAt, ExpiryWarningDays: request.ExpiryWarningDays,
		Source: request.Source, GeneratorKind: request.GeneratorKind, Tags: request.Tags,
		UsageNotes: vaultUsageNotes(request.UsageNotes),
	})
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "vault.item.created", vaultItemAuditPayload(item))
	writeJSON(w, http.StatusCreated, item)
}

func (s vaultItemHandlers) getVaultItem(w http.ResponseWriter, r *http.Request) {
	_, store, ok := s.store(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	item, err := store.Get(r.Context(), id)
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s vaultItemHandlers) updateVaultItem(w http.ResponseWriter, r *http.Request) {
	runtime, store, ok := s.store(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request updateVaultItemRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.writeRequiredVaultAudit(r, runtime, "vault.item.update.requested", map[string]any{
		"project_id":                 request.OwnerProjectID,
		"vault_item_id":              id,
		"expected_metadata_revision": request.ExpectedMetadataRevision,
	}); err != nil {
		writeInternalError(w)
		return
	}
	item, err := store.UpdateMetadata(r.Context(), projectvault.UpdateMetadataInput{
		ID: id, ExpectedMetadataRevision: request.ExpectedMetadataRevision, Name: request.Name,
		OwnerProjectID: request.OwnerProjectID, SharedProjectIDs: request.SharedProjectIDs,
		SecretType: request.SecretType, Provider: request.Provider, Environment: request.Environment,
		Description: request.Description, ExpiresAt: request.ExpiresAt,
		ExpiryWarningDays: request.ExpiryWarningDays, Tags: request.Tags,
		UsageNotes: vaultUsageNotes(request.UsageNotes),
	})
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "vault.item.updated", vaultItemAuditPayload(item))
	writeJSON(w, http.StatusOK, item)
}

func (s vaultItemHandlers) replaceVaultItemValue(w http.ResponseWriter, r *http.Request) {
	runtime, store, ok := s.store(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request replaceVaultItemValueRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	current, err := store.Get(r.Context(), id)
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	if err := s.writeRequiredVaultAudit(r, runtime, "vault.item.value_replace.requested", map[string]any{
		"project_id": current.OwnerProjectID, "vault_item_id": id,
		"expected_value_version": request.ExpectedValueVersion,
	}); err != nil {
		writeInternalError(w)
		return
	}
	item, err := store.ReplaceValue(r.Context(), projectvault.ReplaceValueInput{
		ID: id, Value: request.Value, ExpectedValueVersion: request.ExpectedValueVersion,
	})
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "vault.item.value_replaced", vaultItemAuditPayload(item))
	writeJSON(w, http.StatusOK, item)
}

func (s vaultItemHandlers) revealVaultItem(w http.ResponseWriter, r *http.Request) {
	runtime, store, ok := s.store(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if s.vaultRevealLimiter == nil || !s.vaultRevealLimiter.allow(authRateLimitKey(r, fmt.Sprintf("vault-reveal:%s:%d", runtime.id, id))) {
		writeError(w, http.StatusTooManyRequests, "too many reveal requests; wait before trying again")
		return
	}
	item, err := store.Get(r.Context(), id)
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	value, err := store.Reveal(r.Context(), id)
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	if err := s.writeAuditRequired(r.Context(), runtime, "user", nil, 0, "vault.item.revealed", vaultItemAuditPayload(item)); err != nil {
		writeInternalError(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{"value": value})
}

func (s vaultItemHandlers) deleteVaultItem(w http.ResponseWriter, r *http.Request) {
	runtime, store, ok := s.store(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request deleteVaultItemRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	item, err := store.Get(r.Context(), id)
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	if err := s.writeRequiredVaultAudit(r, runtime, "vault.item.delete.requested", map[string]any{
		"project_id": item.OwnerProjectID, "vault_item_id": id,
		"expected_value_version":     request.ExpectedValueVersion,
		"expected_metadata_revision": request.ExpectedMetadataRevision,
	}); err != nil {
		writeInternalError(w)
		return
	}
	if err := store.Delete(r.Context(), id, request.ExpectedValueVersion, request.ExpectedMetadataRevision); err != nil {
		handleVaultItemError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "vault.item.deleted", vaultItemAuditPayload(item))
	w.WriteHeader(http.StatusNoContent)
}

func (s vaultItemHandlers) store(w http.ResponseWriter, r *http.Request) (*databaseRuntime, *projectvault.Store, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, nil, false
	}
	workspaceUUID, err := projectvault.EnsureWorkspaceUUID(r.Context(), runtime.database)
	if err != nil {
		writeInternalError(w)
		return nil, nil, false
	}
	store, err := projectvault.NewStore(runtime.database, runtime.vault, workspaceUUID)
	if err != nil {
		writeInternalError(w)
		return nil, nil, false
	}
	return runtime, store, true
}

func (s vaultItemHandlers) writeRequiredVaultAudit(r *http.Request, runtime *databaseRuntime, action string, payload map[string]any) error {
	return s.writeAuditRequired(r.Context(), runtime, "user", nil, 0, action, payload)
}

func vaultUsageNotes(values []vaultUsageNoteRequest) []projectvault.UsageNote {
	output := make([]projectvault.UsageNote, 0, len(values))
	for _, value := range values {
		output = append(output, projectvault.UsageNote{Location: value.Location, Notes: value.Notes})
	}
	return output
}

func vaultItemAuditPayload(item projectvault.Item) map[string]any {
	return map[string]any{
		"project_id":        item.OwnerProjectID,
		"vault_item_id":     item.ID,
		"name":              item.Name,
		"value_version":     item.ValueVersion,
		"metadata_revision": item.MetadataRevision,
	}
}

func handleVaultItemError(w http.ResponseWriter, err error) {
	var validation projectvault.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Error())
	case errors.Is(err, projectvault.ErrNotFound):
		writeError(w, http.StatusNotFound, "vault item not found")
	case errors.Is(err, projectvault.ErrStale):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeInternalError(w)
	}
}
