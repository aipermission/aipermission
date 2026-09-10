package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
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

type deleteVaultItemRequest struct {
	ExpectedValueVersion     int64 `json:"expected_value_version"`
	ExpectedMetadataRevision int64 `json:"expected_metadata_revision"`
}

func (s vaultItemHandlers) listVaultItems(w http.ResponseWriter, r *http.Request) {
	_, owner, ok := s.owner(w, r)
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
	items, total, err := owner.List(r.Context(), filter)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s vaultItemHandlers) createVaultItem(w http.ResponseWriter, r *http.Request) {
	_, owner, ok := s.owner(w, r)
	if !ok {
		return
	}
	var request createVaultItemRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	input := projectvault.CreateInput{
		Name: request.Name, Value: request.Value, OwnerProjectID: request.OwnerProjectID,
		SharedProjectIDs: request.SharedProjectIDs, SecretType: request.SecretType,
		Provider: request.Provider, Environment: request.Environment, Description: request.Description,
		ExpiresAt: request.ExpiresAt, ExpiryWarningDays: request.ExpiryWarningDays,
		Source: request.Source, GeneratorKind: request.GeneratorKind, Tags: request.Tags,
		UsageNotes: vaultUsageNotes(request.UsageNotes),
	}
	item, err := owner.Create(r.Context(), input)
	if err != nil {
		if writeVaultDeliveryCancellation(w, err, "Vault item creation was canceled") {
			return
		}
		handleVaultItemError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s vaultItemHandlers) getVaultItem(w http.ResponseWriter, r *http.Request) {
	_, owner, ok := s.owner(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	item, err := owner.Get(r.Context(), id)
	if err != nil {
		handleVaultItemError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s vaultItemHandlers) updateVaultItem(w http.ResponseWriter, r *http.Request) {
	_, owner, ok := s.owner(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request updateVaultItemRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	input := projectvault.UpdateMetadataInput{
		ID: id, ExpectedMetadataRevision: request.ExpectedMetadataRevision, Name: request.Name,
		OwnerProjectID: request.OwnerProjectID, SharedProjectIDs: request.SharedProjectIDs,
		SecretType: request.SecretType, Provider: request.Provider, Environment: request.Environment,
		Description: request.Description, ExpiresAt: request.ExpiresAt,
		ExpiryWarningDays: request.ExpiryWarningDays, Tags: request.Tags,
		UsageNotes: vaultUsageNotes(request.UsageNotes),
	}
	item, err := owner.UpdateMetadata(r.Context(), input)
	if err != nil {
		if writeVaultDeliveryCancellation(w, err, "Vault item update was canceled") {
			return
		}
		handleVaultItemError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s vaultItemHandlers) replaceVaultItemValue(w http.ResponseWriter, r *http.Request) {
	_, owner, ok := s.owner(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request replaceVaultItemValueRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	item, err := owner.ReplaceValue(r.Context(), projectvault.ReplaceRuntimeValueInput{
		ID: id, Value: request.Value, Source: request.Source, GeneratorKind: request.GeneratorKind,
		PreviewToken: request.PreviewToken, ExpectedValueVersion: request.ExpectedValueVersion,
	})
	if err != nil {
		if writeVaultDeliveryCancellation(w, err, "Vault value replacement was canceled") {
			return
		}
		handleVaultItemError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s vaultItemHandlers) generateVaultItemPreview(w http.ResponseWriter, r *http.Request) {
	runtime, owner, ok := s.owner(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request generateVaultItemPreviewRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := owner.GeneratePreview(
		r.Context(), id, request.GeneratorKind,
		runtimecontrol.Key(r, fmt.Sprintf("vault-preview:%s:%d", runtime.id, id)),
	)
	if errors.Is(err, projectvault.ErrGenerateRateLimited) {
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if err != nil {
		if writeVaultDeliveryCancellation(w, err, "Vault item preview generation was canceled") {
			return
		}
		handleVaultItemError(w, err)
		return
	}
	writeSensitiveJSON(w, http.StatusOK, map[string]any{
		"value": result.Value, "preview_token": result.PreviewToken, "generator_kind": result.GeneratorKind,
		"expires_at": result.ExpiresAt.Format(time.RFC3339),
	})
}

func (s vaultItemHandlers) revealVaultItem(w http.ResponseWriter, r *http.Request) {
	runtime, owner, ok := s.owner(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	value, err := owner.Reveal(r.Context(), id, runtimecontrol.Key(r, fmt.Sprintf("vault-reveal:%s:%d", runtime.id, id)))
	if errors.Is(err, projectvault.ErrRevealRateLimited) {
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if err != nil {
		if writeVaultDeliveryCancellation(w, err, "Vault item reveal was canceled") {
			return
		}
		handleVaultItemError(w, err)
		return
	}
	writeSensitiveJSON(w, http.StatusOK, map[string]any{"value": value})
}

func (s vaultItemHandlers) deleteVaultItem(w http.ResponseWriter, r *http.Request) {
	_, owner, ok := s.owner(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request deleteVaultItemRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := owner.Delete(r.Context(), id, request.ExpectedValueVersion, request.ExpectedMetadataRevision); err != nil {
		if writeVaultDeliveryCancellation(w, err, "Vault item deletion was canceled") {
			return
		}
		handleVaultItemError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s vaultItemHandlers) owner(w http.ResponseWriter, r *http.Request) (*databaseRuntime, *projectvault.Runtime, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, nil, false
	}
	owner, err := s.projectVaultRuntime(runtime)
	if err != nil {
		writeInternalError(w)
		return nil, nil, false
	}
	return runtime, owner, true
}

func vaultUsageNotes(values []vaultUsageNoteRequest) []projectvault.UsageNote {
	output := make([]projectvault.UsageNote, 0, len(values))
	for _, value := range values {
		output = append(output, projectvault.UsageNote{Location: value.Location, Notes: value.Notes})
	}
	return output
}

func writeVaultDeliveryCancellation(w http.ResponseWriter, err error, message string) bool {
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	writeError(w, http.StatusRequestTimeout, message)
	return true
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
