package mcpconnector

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

const maxUploadMetadataBytes = 16 << 10

type actionUploadMetadata struct {
	TargetRef      string                        `json:"target_ref"`
	ActionName     string                        `json:"action_name"`
	Reason         string                        `json:"reason"`
	IdempotencyKey string                        `json:"idempotency_key"`
	Manifest       connectors.FileUploadManifest `json:"manifest"`
}

func (h *ActionHTTPHandlers) Upload(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, r, true)
	if !ok {
		return
	}
	if scope.StageUpload == nil || scope.AcquireUploadDispatch == nil || scope.ValidateUpload == nil {
		httptransport.WriteInternalError(w)
		return
	}
	release, ok := h.admit(w, scope)
	if !ok {
		return
	}
	defer release()
	if !scope.Output.MCPStarted() {
		writeActionError(w, r, scope, actions.ErrMCPExecutionStopped)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, connectors.MaximumFileUploadBytes+1<<20)
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(10 * time.Second))
	reader, metadata, err := readUploadMetadata(r)
	if err != nil {
		writeUploadError(w, r, scope, err)
		return
	}
	deadline, _ := r.Context().Deadline()
	_ = controller.SetReadDeadline(deadline)
	h.stageAndDispatchUpload(w, r, scope, reader, metadata)
}

func readUploadMetadata(r *http.Request) (*multipart.Reader, actionUploadMetadata, error) {
	var metadata actionUploadMetadata
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "multipart/form-data" || params["boundary"] == "" || len(params["boundary"]) > 70 {
		return nil, metadata, errUploadMultipart
	}
	reader := multipart.NewReader(r.Body, params["boundary"])
	part, err := reader.NextRawPart()
	if err != nil || part.FormName() != "metadata" {
		return nil, metadata, errUploadMultipart
	}
	body, err := io.ReadAll(io.LimitReader(part, maxUploadMetadataBytes+1))
	if err != nil {
		return nil, metadata, err
	}
	if len(body) > maxUploadMetadataBytes || httptransport.DecodeJSONBytes(body, &metadata) != nil {
		return nil, metadata, errUploadMultipart
	}
	return reader, metadata, nil
}

func (h *ActionHTTPHandlers) stageAndDispatchUpload(w http.ResponseWriter, r *http.Request, scope ActionScope, reader *multipart.Reader, metadata actionUploadMetadata) {
	request := ActionCallRequest{
		TargetRef: metadata.TargetRef, ActionName: metadata.ActionName, Reason: metadata.Reason, IdempotencyKey: metadata.IdempotencyKey,
	}
	if !validateActionCall(w, r, scope, &request) {
		return
	}
	if request.Reason == "" {
		httptransport.WriteError(w, http.StatusBadRequest, "reason is required for uploads")
		return
	}
	if err := metadata.Manifest.ContentIdentity().Validate(); err != nil {
		writeActionError(w, r, scope, err)
		return
	}
	request.Input = metadata.Manifest.ActionInput(request.IdempotencyKey)
	replay, err := scope.ReplayExists(r.Context(), request.IdempotencyKey)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	if replay {
		dispatchUpload(w, r, scope, request, connectors.MaximumActionInputBytes)
		return
	}
	policy, ok := visibleActionPolicy(w, r, scope, request)
	if !ok {
		return
	}
	if policy.MaxUploadBytes <= 0 {
		httptransport.WriteError(w, http.StatusBadRequest, "action does not support streamed uploads")
		return
	}
	if metadata.Manifest.SizeBytes > policy.MaxUploadBytes {
		writeCodedError(w, http.StatusRequestEntityTooLarge, "file exceeds the connector upload limit", "upload_too_large")
		return
	}
	if !actionInputWithinBudget(w, request.Input, policy.MaxInputBytes) {
		return
	}
	if err := scope.ValidateUpload(r.Context(), request.TargetRef, request.ActionName, request.Input, request.Reason); err != nil {
		writeActionError(w, r, scope, err)
		return
	}
	select {
	case h.uploads <- struct{}{}:
		defer func() { <-h.uploads }()
	default:
		writeResourceLimit(w, 5*time.Second)
		return
	}
	if err := stageUploadBody(r, scope, reader, request, metadata.Manifest); err != nil {
		writeUploadError(w, r, scope, err)
		return
	}
	dispatchUpload(w, r.WithContext(connectors.WithStagedUpload(r.Context())), scope, request, policy.MaxInputBytes)
}

func dispatchUpload(w http.ResponseWriter, r *http.Request, scope ActionScope, request ActionCallRequest, maxInputBytes int) {
	release, err := scope.AcquireUploadDispatch(r.Context())
	if err != nil || release == nil {
		httptransport.WriteError(w, http.StatusServiceUnavailable, "upload dispatch workspace is unavailable")
		return
	}
	defer release()
	dispatchAction(w, r, scope, request, maxInputBytes)
}

func stageUploadBody(r *http.Request, scope ActionScope, reader *multipart.Reader, request ActionCallRequest, manifest connectors.FileUploadManifest) error {
	part, err := reader.NextRawPart()
	if err != nil || part.FormName() != "file" || part.Header.Get("Content-Transfer-Encoding") != "" {
		return errUploadMultipart
	}
	store := connectortargets.NewStore(scope.Database)
	target, profile, err := store.ResolveConnectorActionTarget(r.Context(), request.TargetRef)
	if err != nil {
		return err
	}
	surface, err := store.EnsureRuntimeSurface(r.Context(), connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: target.ConnectorKind, TargetID: target.ID, ProfileID: profile.ID,
		CapabilityKind: connectortargets.RuntimeCapabilityFileTransfer, Label: profile.Label,
	})
	if err != nil {
		return err
	}
	return scope.StageUpload(r.Context(), connectors.FileUploadStageRequest{
		RuntimeID: surface.ID, Ref: request.IdempotencyKey, TargetRef: request.TargetRef,
		ActionName: request.ActionName, Manifest: manifest,
	}, &finalUploadPart{part: part, reader: reader})
}

var errUploadMultipart = errors.New("upload requires a metadata part followed by exactly one binary file part")

type finalUploadPart struct {
	part   *multipart.Part
	reader *multipart.Reader
}

func (part *finalUploadPart) Read(buffer []byte) (int, error) {
	size, err := part.part.Read(buffer)
	if err == io.EOF {
		if _, end := part.reader.NextRawPart(); end != io.EOF {
			return size, errUploadMultipart
		}
	}
	if err != nil && err != io.EOF {
		return size, errors.Join(errUploadMultipart, err)
	}
	return size, err
}

func writeUploadError(w http.ResponseWriter, r *http.Request, scope ActionScope, err error) {
	var limit *http.MaxBytesError
	switch {
	case errors.As(err, &limit):
		writeCodedError(w, http.StatusRequestEntityTooLarge, "upload body is too large", "upload_too_large")
	case errors.Is(err, connectors.ErrUploadStagingCapacity):
		writeCodedError(w, http.StatusTooManyRequests, err.Error(), "upload_staging_capacity")
	case errors.Is(err, connectors.ErrUploadStageConflict):
		httptransport.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, connectors.ErrUploadStageExpired):
		writeCodedError(w, http.StatusConflict, connectors.ErrUploadStageExpired.Error(), "upload_stage_expired")
	case errors.Is(err, errUploadMultipart):
		httptransport.WriteError(w, http.StatusBadRequest, errUploadMultipart.Error())
	case errors.Is(err, connectors.ErrUploadContentChanged), errors.Is(err, connectors.ErrInvalidUploadManifest), errors.Is(err, connectortargets.ErrTargetProfileNotFound):
		writeActionError(w, r, scope, err)
	default:
		httptransport.WriteInternalError(w)
	}
}
