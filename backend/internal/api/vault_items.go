package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
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
	Source               string `json:"source"`
	GeneratorKind        string `json:"generator_kind"`
	PreviewToken         string `json:"preview_token"`
	ExpectedValueVersion int64  `json:"expected_value_version"`
}

type generateVaultItemPreviewRequest struct {
	GeneratorKind string `json:"generator_kind"`
}

type generatedVaultItemPreview struct {
	ItemID               int64          `json:"item_id"`
	ExpectedValueVersion int64          `json:"expected_value_version"`
	GeneratorKind        string         `json:"generator_kind"`
	GeneratorParameters  map[string]any `json:"generator_parameters"`
	Value                string         `json:"value"`
	ExpiresAtUnix        int64          `json:"expires_at_unix"`
}

type deleteVaultItemRequest struct {
	ExpectedValueVersion     int64 `json:"expected_value_version"`
	ExpectedMetadataRevision int64 `json:"expected_metadata_revision"`
}

func (s vaultItemHandlers) listVaultItems(w http.ResponseWriter, r *http.Request) {
	_, store, ok := s.store(w, r)
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
	release, err := runtime.vaultDelivery.acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "Vault item update was canceled")
		return
	}
	defer release()
	current, err := store.Get(r.Context(), id)
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	if current.MetadataRevision != request.ExpectedMetadataRevision {
		handleVaultItemError(w, projectvault.ErrStale)
		return
	}
	scope := projectvault.SessionMutationScope{ItemID: id}
	sessions, err := store.ActiveSessionsForMutation(r.Context(), scope)
	if err != nil {
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
	if err := invalidateVaultMutationAfterCommit(r.Context(), runtime, sessions, scope); err != nil {
		writeInternalError(w)
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
	request.Source = strings.TrimSpace(request.Source)
	request.GeneratorKind = strings.TrimSpace(request.GeneratorKind)
	if request.Source == "" {
		request.Source = "imported"
	}
	if request.Source == "generated" {
		if request.Value != "" {
			writeError(w, http.StatusBadRequest, "generated replacement cannot include an imported value")
			return
		}
		if request.PreviewToken == "" {
			writeError(w, http.StatusBadRequest, "generated replacement preview is required")
			return
		}
		if err := projectvault.ValidateGeneratorKind(request.GeneratorKind); err != nil {
			handleVaultItemError(w, err)
			return
		}
	} else if request.Source != "imported" || request.GeneratorKind != "" || request.PreviewToken != "" {
		writeError(w, http.StatusBadRequest, "source must select an imported value or a supported generator")
		return
	}
	release, err := runtime.vaultDelivery.acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "Vault value replacement was canceled")
		return
	}
	defer release()
	current, err := store.Get(r.Context(), id)
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	if err := s.writeRequiredVaultAudit(r, runtime, "vault.item.value_replace.requested", map[string]any{
		"project_id": current.OwnerProjectID, "vault_item_id": id,
		"expected_value_version": request.ExpectedValueVersion,
		"source":                 request.Source,
		"generator_kind":         request.GeneratorKind,
	}); err != nil {
		writeInternalError(w)
		return
	}
	if current.ValueVersion != request.ExpectedValueVersion {
		handleVaultItemError(w, projectvault.ErrStale)
		return
	}
	var generatedPreview generatedVaultItemPreview
	if request.Source == "generated" {
		aad := vaultItemPreviewAAD(runtime.workspaceUUID, id, request.ExpectedValueVersion)
		if runtime.vault == nil || runtime.vault.DecryptJSONWithAAD(request.PreviewToken, &generatedPreview, aad) != nil ||
			generatedPreview.ItemID != id ||
			generatedPreview.ExpectedValueVersion != request.ExpectedValueVersion ||
			generatedPreview.GeneratorKind != request.GeneratorKind ||
			generatedPreview.ExpiresAtUnix <= time.Now().UTC().Unix() {
			writeError(w, http.StatusBadRequest, "generated replacement preview is invalid or expired")
			return
		}
		request.Value = generatedPreview.Value
	}
	scope := projectvault.SessionMutationScope{ItemID: id}
	sessions, err := store.ActiveSessionsForMutation(r.Context(), scope)
	if err != nil {
		writeInternalError(w)
		return
	}
	item, err := store.ReplaceValue(r.Context(), projectvault.ReplaceValueInput{
		ID: id, Value: request.Value, Source: request.Source, GeneratorKind: request.GeneratorKind,
		GeneratorParams:      generatedPreview.GeneratorParameters,
		ExpectedValueVersion: request.ExpectedValueVersion,
	})
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	if err := invalidateVaultMutationAfterCommit(r.Context(), runtime, sessions, scope); err != nil {
		writeInternalError(w)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "vault.item.value_replaced", vaultItemAuditPayload(item))
	writeJSON(w, http.StatusOK, item)
}

func (s vaultItemHandlers) generateVaultItemPreview(w http.ResponseWriter, r *http.Request) {
	runtime, store, ok := s.store(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request generateVaultItemPreviewRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.GeneratorKind = strings.TrimSpace(request.GeneratorKind)
	if err := projectvault.ValidateGeneratorKind(request.GeneratorKind); err != nil {
		handleVaultItemError(w, err)
		return
	}
	release, err := runtime.vaultDelivery.acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "Vault item preview generation was canceled")
		return
	}
	defer release()
	item, err := store.Get(r.Context(), id)
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	if s.vaultGenerateLimiter == nil || !s.vaultGenerateLimiter.allow(authRateLimitKey(r, fmt.Sprintf("vault-preview:%s:%d", runtime.id, id))) {
		writeError(w, http.StatusTooManyRequests, "too many generated previews; wait before trying again")
		return
	}
	value, parameters, err := projectvault.Generate(request.GeneratorKind)
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	expiresAt := time.Now().UTC().Add(5 * time.Minute)
	preview := generatedVaultItemPreview{
		ItemID: id, ExpectedValueVersion: item.ValueVersion, GeneratorKind: request.GeneratorKind,
		GeneratorParameters: parameters, Value: value, ExpiresAtUnix: expiresAt.Unix(),
	}
	if runtime.vault == nil {
		writeInternalError(w)
		return
	}
	previewToken, err := runtime.vault.EncryptJSONWithAAD(preview, vaultItemPreviewAAD(runtime.workspaceUUID, id, item.ValueVersion))
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := s.writeAuditRequired(r.Context(), runtime, "user", nil, 0, "vault.item.value_preview.generated", map[string]any{
		"project_id": item.OwnerProjectID, "vault_item_id": id,
		"expected_value_version": item.ValueVersion, "generator_kind": request.GeneratorKind,
		"expires_at": expiresAt.Format(time.RFC3339),
	}); err != nil {
		writeInternalError(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{
		"value": value, "preview_token": previewToken, "generator_kind": request.GeneratorKind,
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

func vaultItemPreviewAAD(workspaceUUID string, itemID, valueVersion int64) []byte {
	return []byte(fmt.Sprintf("project-vault-value-preview:v1:%s:%d:%d", workspaceUUID, itemID, valueVersion))
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
	release, err := runtime.vaultDelivery.acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "Vault item deletion was canceled")
		return
	}
	defer release()
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
	if item.ValueVersion != request.ExpectedValueVersion || item.MetadataRevision != request.ExpectedMetadataRevision {
		handleVaultItemError(w, projectvault.ErrStale)
		return
	}
	sessions, err := store.ActiveSessionsForMutation(r.Context(), projectvault.SessionMutationScope{ItemID: item.ID})
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := store.Delete(r.Context(), id, request.ExpectedValueVersion, request.ExpectedMetadataRevision); err != nil {
		handleVaultItemError(w, err)
		return
	}
	if err := invalidateVaultMutationAfterCommit(r.Context(), runtime, sessions, projectvault.SessionMutationScope{ItemID: item.ID}); err != nil {
		writeInternalError(w)
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
	case errors.Is(err, connectors.ErrSessionEnvironmentUnsupported):
		writeError(w, http.StatusConflict, "this connector runtime does not support Vault session environments")
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
