package api

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/auditoutbox"
)

var errAuditedMutationUnchanged = errors.New("audited mutation unchanged")

func (s *Server) auditHealthSnapshot(ctx context.Context) auditoutbox.HealthSnapshot {
	runtime := s.activeRuntime()
	if runtime == nil {
		return s.auditHealth.Snapshot(ctx, nil)
	}
	return s.auditHealth.Snapshot(ctx, runtime.database)
}

func (s auditHandlers) listAuditLogs(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	page, err := parsePageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	actor := strings.TrimSpace(r.URL.Query().Get("actor"))
	var runtimeID int64
	if rawRuntimeID := strings.TrimSpace(r.URL.Query().Get("runtime_id")); rawRuntimeID != "" {
		id, ok := parseInt64Query(w, rawRuntimeID, "runtime_id")
		if !ok {
			return
		}
		runtimeID = id
	}
	var projectID int64
	if rawProjectID := strings.TrimSpace(r.URL.Query().Get("project_id")); rawProjectID != "" {
		id, ok := parseInt64Query(w, rawProjectID, "project_id")
		if !ok {
			return
		}
		projectID = id
	}
	connectorKind := strings.TrimSpace(r.URL.Query().Get("connector_kind"))
	var targetID int64
	if rawTargetID := strings.TrimSpace(r.URL.Query().Get("target_id")); rawTargetID != "" {
		id, ok := parseInt64Query(w, rawTargetID, "target_id")
		if !ok {
			return
		}
		targetID = id
	}
	result, err := auditoutbox.NewQueryStore(runtime.database).List(r.Context(), auditoutbox.QueryFilter{
		Actor:         actor,
		RuntimeID:     runtimeID,
		ProjectID:     projectID,
		ConnectorKind: connectorKind,
		TargetID:      targetID,
		Query:         page.Query,
		Limit:         page.Limit,
		Offset:        page.Offset,
	})
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, makePageResponse(result.Items, result.Total, page))
}

func (s auditHandlers) getAuditLog(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	item, err := auditoutbox.NewQueryStore(runtime.database).Get(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "audit log not found")
		return
	}
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// writeObservationAudit records telemetry that is not the durable proof of a
// local domain mutation. Mutations must use withAuditedMutation, a
// transaction-aware store hook, or an approved lifecycle trigger instead.
func (s *Server) writeObservationAudit(ctx context.Context, runtime *databaseRuntime, actorType string, tokenID *int64, runtimeID int64, action string, payload any) {
	if err := s.writeAuditRequired(ctx, runtime, actorType, tokenID, runtimeID, action, payload); err != nil {
		log.Printf("audit write failed actor=%q runtime_id=%d action=%q error=%v", actorType, runtimeID, action, err)
	}
}

func (s *Server) writeAuditRequired(ctx context.Context, runtime *databaseRuntime, actorType string, tokenID *int64, runtimeID int64, action string, payload any) (err error) {
	defer func() {
		if err != nil && s != nil {
			s.auditHealth.RecordFailure(time.Now())
		}
	}()
	return s.auditedWriteCoordinator(ctx, runtime).WriteRequired(ctx, actorType, tokenID, runtimeID, action, payload)
}

func (s *Server) prepareAuditRedactor(ctx context.Context, runtime *databaseRuntime) func(string) string {
	if s.redactionMode(ctx, runtime) == redactionModeOff {
		return func(value string) string { return value }
	}
	rules, _ := s.compiledRedactionRules(ctx, runtime)
	return func(value string) string {
		value = redactBasic(value)
		for _, rule := range rules {
			value = rule.Regex.ReplaceAllString(value, "[REDACTED]")
		}
		return value
	}
}

type auditAppender = auditoutbox.Appender

func (s *Server) withAuditedTransaction(
	ctx context.Context,
	runtime *databaseRuntime,
	mutate func(*sql.Tx, auditAppender) error,
) error {
	return s.auditedWriteCoordinator(ctx, runtime).WithTransaction(ctx, mutate)
}

func (s *Server) withAuditedMutation(
	ctx context.Context,
	runtime *databaseRuntime,
	actorType string,
	tokenID *int64,
	runtimeID int64,
	action string,
	payload func() any,
	mutate func(*sql.Tx) error,
) error {
	return s.auditedWriteCoordinator(ctx, runtime).WithMutation(ctx, actorType, tokenID, runtimeID, action, payload, mutate)
}

func (s *Server) projectAuditEvents(ctx context.Context, runtime *databaseRuntime) {
	if runtime == nil {
		return
	}
	s.newAuditCoordinator(runtime, nil).Project(ctx)
}

func (s *Server) auditedWriteCoordinator(ctx context.Context, runtime *databaseRuntime) *auditoutbox.Coordinator {
	if runtime == nil || runtime.database == nil {
		return auditoutbox.NewCoordinator(nil, nil, nil, s.auditProjectionFailureHandler())
	}
	// Redaction policy reads must happen before a transaction reserves
	// SQLCipher's single database connection.
	redact := s.prepareAuditRedactor(ctx, runtime)
	return s.newAuditCoordinator(runtime, redact)
}

func (s *Server) newAuditCoordinator(runtime *databaseRuntime, redact func(string) string) *auditoutbox.Coordinator {
	return auditoutbox.NewCoordinator(runtime.database, runtime.auditDispatcher, redact, s.auditProjectionFailureHandler())
}

func (s *Server) auditProjectionFailureHandler() auditoutbox.ProjectionFailureHandler {
	return func(action string, err error) {
		if s != nil {
			s.auditHealth.RecordFailure(time.Now())
		}
		if action == "" {
			log.Printf("audit projection failed error=%v", err)
			return
		}
		log.Printf("audit projection failed action=%q error=%v", action, err)
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
