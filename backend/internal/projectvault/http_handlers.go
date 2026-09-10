package projectvault

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
)

type HTTPScope struct {
	Runtime        *Runtime
	RuntimeID      string
	SessionCatalog SessionOptionsCatalog
}

type HTTPScopeProvider func(http.ResponseWriter) (HTTPScope, bool)

type HTTPHandlers struct {
	scope HTTPScopeProvider
}

type UsageNoteHTTPRequest struct {
	Location string `json:"location"`
	Notes    string `json:"notes"`
}

type CreateHTTPRequest struct {
	Name              string                 `json:"name"`
	Value             string                 `json:"value"`
	OwnerProjectID    int64                  `json:"owner_project_id"`
	SharedProjectIDs  []int64                `json:"shared_project_ids"`
	SecretType        string                 `json:"secret_type"`
	Provider          string                 `json:"provider"`
	Environment       string                 `json:"environment"`
	Description       string                 `json:"description"`
	ExpiresAt         string                 `json:"expires_at"`
	ExpiryWarningDays int                    `json:"expiry_warning_days"`
	Source            string                 `json:"source"`
	GeneratorKind     string                 `json:"generator_kind"`
	Tags              []string               `json:"tags"`
	UsageNotes        []UsageNoteHTTPRequest `json:"usage_notes"`
}

type UpdateHTTPRequest struct {
	ExpectedMetadataRevision int64                  `json:"expected_metadata_revision"`
	Name                     string                 `json:"name"`
	OwnerProjectID           int64                  `json:"owner_project_id"`
	SharedProjectIDs         []int64                `json:"shared_project_ids"`
	SecretType               string                 `json:"secret_type"`
	Provider                 string                 `json:"provider"`
	Environment              string                 `json:"environment"`
	Description              string                 `json:"description"`
	ExpiresAt                string                 `json:"expires_at"`
	ExpiryWarningDays        int                    `json:"expiry_warning_days"`
	Tags                     []string               `json:"tags"`
	UsageNotes               []UsageNoteHTTPRequest `json:"usage_notes"`
}

type ReplaceValueHTTPRequest struct {
	Value                string `json:"value"`
	Source               string `json:"source"`
	GeneratorKind        string `json:"generator_kind"`
	PreviewToken         string `json:"preview_token"`
	ExpectedValueVersion int64  `json:"expected_value_version"`
}

type GeneratePreviewHTTPRequest struct {
	GeneratorKind string `json:"generator_kind"`
}

type DeleteHTTPRequest struct {
	ExpectedValueVersion     int64 `json:"expected_value_version"`
	ExpectedMetadataRevision int64 `json:"expected_metadata_revision"`
}

type SaveDefaultBindingHTTPRequest struct {
	VaultItemID             int64 `json:"vault_item_id"`
	SourceProjectID         int64 `json:"source_project_id"`
	TargetID                int64 `json:"target_id"`
	ProfileID               int64 `json:"profile_id"`
	ReplaceExisting         bool  `json:"replace_existing"`
	ExpectedBindingRevision int64 `json:"expected_binding_revision"`
}

type DeleteDefaultBindingHTTPRequest struct {
	ExpectedBindingRevision int64 `json:"expected_binding_revision"`
}

func NewHTTPHandlers(scope HTTPScopeProvider) *HTTPHandlers {
	return &HTTPHandlers{scope: scope}
}

func (h *HTTPHandlers) ListItems(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w)
	if !ok {
		return
	}
	filter := ListFilter{Query: strings.TrimSpace(r.URL.Query().Get("q"))}
	var err error
	if filter.ProjectID, err = optionalPositiveQueryID(r, "project_id"); err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if filter.Limit, err = optionalQueryInt(r, "limit"); err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if filter.Offset, err = optionalQueryInt(r, "offset"); err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, total, err := scope.Runtime.List(r.Context(), filter)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (h *HTTPHandlers) CreateItem(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w)
	if !ok {
		return
	}
	var request CreateHTTPRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	item, err := scope.Runtime.Create(r.Context(), CreateInput{
		Name: request.Name, Value: request.Value, OwnerProjectID: request.OwnerProjectID,
		SharedProjectIDs: request.SharedProjectIDs, SecretType: request.SecretType,
		Provider: request.Provider, Environment: request.Environment, Description: request.Description,
		ExpiresAt: request.ExpiresAt, ExpiryWarningDays: request.ExpiryWarningDays,
		Source: request.Source, GeneratorKind: request.GeneratorKind, Tags: request.Tags,
		UsageNotes: httpUsageNotes(request.UsageNotes),
	})
	if err != nil {
		if writeHTTPCancellation(w, err, "Vault item creation was canceled") {
			return
		}
		writeItemHTTPError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusCreated, item)
}

func (h *HTTPHandlers) GetItem(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := h.itemScope(w, r)
	if !ok {
		return
	}
	item, err := scope.Runtime.Get(r.Context(), id)
	if err != nil {
		writeItemHTTPError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, item)
}

func (h *HTTPHandlers) UpdateItem(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := h.itemScope(w, r)
	if !ok {
		return
	}
	var request UpdateHTTPRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	item, err := scope.Runtime.UpdateMetadata(r.Context(), UpdateMetadataInput{
		ID: id, ExpectedMetadataRevision: request.ExpectedMetadataRevision, Name: request.Name,
		OwnerProjectID: request.OwnerProjectID, SharedProjectIDs: request.SharedProjectIDs,
		SecretType: request.SecretType, Provider: request.Provider, Environment: request.Environment,
		Description: request.Description, ExpiresAt: request.ExpiresAt,
		ExpiryWarningDays: request.ExpiryWarningDays, Tags: request.Tags,
		UsageNotes: httpUsageNotes(request.UsageNotes),
	})
	if err != nil {
		if writeHTTPCancellation(w, err, "Vault item update was canceled") {
			return
		}
		writeItemHTTPError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, item)
}

func (h *HTTPHandlers) ReplaceItemValue(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := h.itemScope(w, r)
	if !ok {
		return
	}
	var request ReplaceValueHTTPRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	item, err := scope.Runtime.ReplaceValue(r.Context(), ReplaceRuntimeValueInput{
		ID: id, Value: request.Value, Source: request.Source, GeneratorKind: request.GeneratorKind,
		PreviewToken: request.PreviewToken, ExpectedValueVersion: request.ExpectedValueVersion,
	})
	if err != nil {
		if writeHTTPCancellation(w, err, "Vault value replacement was canceled") {
			return
		}
		writeItemHTTPError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, item)
}

func (h *HTTPHandlers) GenerateItemPreview(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := h.itemScope(w, r)
	if !ok {
		return
	}
	var request GeneratePreviewHTTPRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	result, err := scope.Runtime.GeneratePreview(
		r.Context(), id, request.GeneratorKind,
		runtimecontrol.Key(r, fmt.Sprintf("vault-preview:%s:%d", scope.RuntimeID, id)),
	)
	if errors.Is(err, ErrGenerateRateLimited) {
		httptransport.WriteError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if err != nil {
		if writeHTTPCancellation(w, err, "Vault item preview generation was canceled") {
			return
		}
		writeItemHTTPError(w, err)
		return
	}
	httptransport.WriteSensitiveJSON(w, http.StatusOK, map[string]any{
		"value": result.Value, "preview_token": result.PreviewToken, "generator_kind": result.GeneratorKind,
		"expires_at": result.ExpiresAt.Format(time.RFC3339),
	})
}

func (h *HTTPHandlers) RevealItem(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := h.itemScope(w, r)
	if !ok {
		return
	}
	value, err := scope.Runtime.Reveal(
		r.Context(), id,
		runtimecontrol.Key(r, fmt.Sprintf("vault-reveal:%s:%d", scope.RuntimeID, id)),
	)
	if errors.Is(err, ErrRevealRateLimited) {
		httptransport.WriteError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if err != nil {
		if writeHTTPCancellation(w, err, "Vault item reveal was canceled") {
			return
		}
		writeItemHTTPError(w, err)
		return
	}
	httptransport.WriteSensitiveJSON(w, http.StatusOK, map[string]any{"value": value})
}

func (h *HTTPHandlers) DeleteItem(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := h.itemScope(w, r)
	if !ok {
		return
	}
	var request DeleteHTTPRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	if err := scope.Runtime.Delete(r.Context(), id, request.ExpectedValueVersion, request.ExpectedMetadataRevision); err != nil {
		if writeHTTPCancellation(w, err, "Vault item deletion was canceled") {
			return
		}
		writeItemHTTPError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandlers) ListDefaultBindings(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w)
	if !ok {
		return
	}
	itemID, err := optionalPositiveQueryID(r, "vault_item_id")
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	targetID, err := optionalPositiveQueryID(r, "target_id")
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	profileID, err := optionalPositiveQueryID(r, "profile_id")
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := scope.Runtime.ListDefaultBindings(r.Context(), DefaultBindingFilter{
		VaultItemID: itemID, TargetID: targetID, ProfileID: profileID,
	})
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *HTTPHandlers) SaveDefaultBinding(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w)
	if !ok {
		return
	}
	var request SaveDefaultBindingHTTPRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	item, err := scope.Runtime.SaveDefaultBinding(r.Context(), DefaultBindingInput{
		VaultItemID: request.VaultItemID, SourceProjectID: request.SourceProjectID,
		TargetID: request.TargetID, ProfileID: request.ProfileID,
		ReplaceExisting: request.ReplaceExisting, ExpectedBindingRevision: request.ExpectedBindingRevision,
	})
	if err != nil {
		if writeHTTPCancellation(w, err, "Vault binding update was canceled") {
			return
		}
		writeBindingHTTPError(w, err)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, item)
}

func (h *HTTPHandlers) DeleteDefaultBinding(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := h.itemScope(w, r)
	if !ok {
		return
	}
	var request DeleteDefaultBindingHTTPRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	if err := scope.Runtime.DeleteDefaultBinding(r.Context(), id, request.ExpectedBindingRevision); err != nil {
		if writeHTTPCancellation(w, err, "Vault binding deletion was canceled") {
			return
		}
		writeBindingHTTPError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandlers) resolve(w http.ResponseWriter) (HTTPScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return HTTPScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return HTTPScope{}, false
	}
	if scope.Runtime == nil || strings.TrimSpace(scope.RuntimeID) == "" {
		httptransport.WriteInternalError(w)
		return HTTPScope{}, false
	}
	return scope, true
}

func (h *HTTPHandlers) itemScope(w http.ResponseWriter, r *http.Request) (HTTPScope, int64, bool) {
	scope, ok := h.resolve(w)
	if !ok {
		return HTTPScope{}, 0, false
	}
	id, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	return scope, id, ok
}

func optionalPositiveQueryID(r *http.Request, name string) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return 0, ValidationError(name + " must be a positive integer")
	}
	return value, nil
}

func optionalQueryInt(r *http.Request, name string) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, ValidationError(name + " must be an integer")
	}
	return value, nil
}

func httpUsageNotes(values []UsageNoteHTTPRequest) []UsageNote {
	output := make([]UsageNote, 0, len(values))
	for _, value := range values {
		output = append(output, UsageNote{Location: value.Location, Notes: value.Notes})
	}
	return output
}

func writeHTTPCancellation(w http.ResponseWriter, err error, message string) bool {
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	httptransport.WriteError(w, http.StatusRequestTimeout, message)
	return true
}

func writeItemHTTPError(w http.ResponseWriter, err error) {
	var validation ValidationError
	switch {
	case errors.Is(err, ErrSessionEnvironmentUnsupported):
		httptransport.WriteError(w, http.StatusConflict, "this connector runtime does not support Vault session environments")
	case errors.As(err, &validation):
		httptransport.WriteError(w, http.StatusBadRequest, validation.Error())
	case errors.Is(err, ErrNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "vault item not found")
	case errors.Is(err, ErrStale):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	default:
		httptransport.WriteInternalError(w)
	}
}

func writeBindingHTTPError(w http.ResponseWriter, err error) {
	var validation ValidationError
	switch {
	case errors.As(err, &validation):
		httptransport.WriteError(w, http.StatusBadRequest, validation.Error())
	case errors.Is(err, ErrNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "vault default binding not found")
	case errors.Is(err, ErrBindingTargetNotFound):
		httptransport.WriteError(w, http.StatusNotFound, "connector target not found")
	case errors.Is(err, ErrSessionEnvironmentUnsupported):
		httptransport.WriteError(w, http.StatusConflict, "this connector profile does not support Vault session environments")
	case errors.Is(err, ErrStale):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	default:
		httptransport.WriteInternalError(w)
	}
}
