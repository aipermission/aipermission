package connectormanagement

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type ProfileCleanupOutcome struct {
	Required bool
	Status   string
	Output   any
}

type ProfileDeletionScope struct {
	Database             *sql.DB
	AcquireExclusive     func(context.Context) (func(), error)
	Cleanup              func(context.Context, connectortargets.Target, connectortargets.CredentialProfile) (ProfileCleanupOutcome, error)
	BeforeDelete         func(context.Context, connectortargets.Target, connectortargets.CredentialProfile) error
	WithTransaction      func(context.Context, func(*sql.Tx, AuditAppender) error) error
	AfterLifecycleChange func(context.Context, TargetLifecycleChange) error
}

type ProfileDeletionScopeProvider func(http.ResponseWriter) (ProfileDeletionScope, bool)

type ProfileDeletionHTTPHandler struct{ scope ProfileDeletionScopeProvider }

var errProfileDeletionRuntimeUnavailable = errors.New("connector profile deletion runtime is unavailable")

func NewProfileDeletionHTTPHandler(scope ProfileDeletionScopeProvider) *ProfileDeletionHTTPHandler {
	return &ProfileDeletionHTTPHandler{scope: scope}
}

func (h *ProfileDeletionHTTPHandler) Delete(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w)
	if !ok {
		return
	}
	targetID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	profileID, ok := httptransport.ParsePathInt64(w, r, "profile_id", "profile_id is required")
	if !ok {
		return
	}
	release, err := scope.AcquireExclusive(r.Context())
	if err != nil {
		httptransport.WriteError(w, http.StatusRequestTimeout, "connector credential profile deletion was canceled")
		return
	}
	if release == nil {
		httptransport.WriteInternalError(w)
		return
	}
	defer release()
	store := connectortargets.NewStore(scope.Database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	profile, err := store.GetCredentialProfile(r.Context(), targetID, profileID)
	if err != nil {
		writeTargetError(w, err)
		return
	}

	cleanup, err := scope.Cleanup(r.Context(), target, profile)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	if err := scope.BeforeDelete(r.Context(), target, profile); err != nil {
		writeTargetError(w, err)
		return
	}
	err = scope.WithTransaction(r.Context(), func(tx *sql.Tx, appendAudit AuditAppender) error {
		if tx == nil || appendAudit == nil {
			return errProfileDeletionRuntimeUnavailable
		}
		if err := connectortargets.NewTxStore(tx).DeleteCredentialProfile(r.Context(), targetID, profileID); err != nil {
			return err
		}
		payload := profileAuditPayload(target, profile)
		if cleanup.Required {
			payload["external_cleanup"] = map[string]any{"status": cleanup.Status, "output": cleanup.Output}
		}
		return appendAudit(tx, "user", nil, 0, "connector.profile.deleted", payload)
	})
	if err != nil {
		writeTargetError(w, err)
		return
	}
	if err := scope.AfterLifecycleChange(r.Context(), TargetLifecycleChange{
		TargetID: targetID, ProfileID: profileID,
		StaleReason:    "connector credential profile was deleted; send a fresh Vault request",
		UserMessage:    "connector credential profile was deleted; ask the AI to send a fresh request",
		IncludeRunning: true,
	}); err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ProfileDeletionHTTPHandler) resolve(w http.ResponseWriter) (ProfileDeletionScope, bool) {
	if h == nil || h.scope == nil {
		httptransport.WriteInternalError(w)
		return ProfileDeletionScope{}, false
	}
	scope, ok := h.scope(w)
	if !ok {
		return ProfileDeletionScope{}, false
	}
	if scope.Database == nil || scope.AcquireExclusive == nil || scope.Cleanup == nil ||
		scope.BeforeDelete == nil || scope.WithTransaction == nil || scope.AfterLifecycleChange == nil {
		httptransport.WriteInternalError(w)
		return ProfileDeletionScope{}, false
	}
	return scope, true
}
