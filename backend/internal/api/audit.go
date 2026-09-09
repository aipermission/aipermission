package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/auditoutbox"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

var errAuditedMutationUnchanged = errors.New("audited mutation unchanged")

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
			s.auditHealth.recordFailure(time.Now())
		}
	}()
	if runtime == nil || runtime.database == nil {
		return fmt.Errorf("audit database is unavailable")
	}
	event, err := s.buildAuditEvent(ctx, runtime, runtime.database, actorType, tokenID, runtimeID, action, payload)
	if err != nil {
		return err
	}
	_, err = (auditoutbox.Store{}).Append(ctx, runtime.database, event)
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	dispatcher := runtime.auditDispatcher
	if dispatcher == nil {
		return nil
	}
	if _, dispatchErr := dispatcher.DispatchOnce(ctx); dispatchErr != nil {
		s.auditHealth.recordFailure(time.Now())
		log.Printf("audit projection failed action=%q error=%v", action, dispatchErr)
		dispatcher.Notify()
	}
	return nil
}

func (s *Server) buildAuditEvent(
	ctx context.Context,
	runtime *databaseRuntime,
	executor sqldb.Executor,
	actorType string,
	tokenID *int64,
	runtimeID int64,
	action string,
	payload any,
) (auditoutbox.Event, error) {
	redact := s.prepareAuditRedactor(ctx, runtime)
	return s.buildAuditEventWithRedactor(ctx, executor, actorType, tokenID, runtimeID, action, payload, redact)
}

func (s *Server) buildAuditEventWithRedactor(
	ctx context.Context,
	executor sqldb.Executor,
	actorType string,
	tokenID *int64,
	runtimeID int64,
	action string,
	payload any,
	redact func(string) string,
) (auditoutbox.Event, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return auditoutbox.Event{}, fmt.Errorf("marshal audit payload: %w", err)
	}
	payloadJSON := redact(string(payloadBytes))
	connectorKind, projectID, targetID, profileID, actionRequestID := auditConnectorMetadata(payload)
	projectID = resolveAuditProjectID(ctx, executor, projectID, targetID, runtimeID)
	return auditoutbox.Event{
		ActorType:       actorType,
		TokenID:         tokenID,
		ProjectID:       projectID,
		RuntimeID:       runtimeID,
		ConnectorKind:   connectorKind,
		TargetID:        targetID,
		ProfileID:       profileID,
		ActionRequestID: actionRequestID,
		Action:          action,
		LifecyclePhase:  auditLifecyclePhase(action),
		PayloadJSON:     payloadJSON,
	}, nil
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

type auditAppender func(*sql.Tx, string, *int64, int64, string, any) error

func (s *Server) prepareAuditAppender(ctx context.Context, runtime *databaseRuntime) auditAppender {
	redact := s.prepareAuditRedactor(ctx, runtime)
	return func(tx *sql.Tx, actorType string, tokenID *int64, runtimeID int64, action string, payload any) error {
		event, err := s.buildAuditEventWithRedactor(ctx, tx, actorType, tokenID, runtimeID, action, payload, redact)
		if err != nil {
			return err
		}
		_, err = (auditoutbox.Store{}).Append(ctx, tx, event)
		return err
	}
}

func (s *Server) withAuditedTransaction(
	ctx context.Context,
	runtime *databaseRuntime,
	mutate func(*sql.Tx, auditAppender) error,
) error {
	if runtime == nil || runtime.database == nil {
		return fmt.Errorf("audit database is unavailable")
	}
	// Preparing the redactor reads runtime configuration. Do that before the
	// transaction reserves SQLCipher's single database connection.
	appendAudit := s.prepareAuditAppender(ctx, runtime)
	tx, err := runtime.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin audited mutation: %w", err)
	}
	defer tx.Rollback()
	if err := mutate(tx, appendAudit); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit audited mutation: %w", err)
	}
	s.projectAuditEvents(ctx, runtime)
	return nil
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
	if runtime == nil || runtime.database == nil {
		return fmt.Errorf("audit database is unavailable")
	}
	return s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
		if err := mutate(tx); err != nil {
			return err
		}
		return appendAudit(tx, actorType, tokenID, runtimeID, action, payload())
	})
}

func (s *Server) projectAuditEvents(ctx context.Context, runtime *databaseRuntime) {
	if runtime == nil || runtime.database == nil {
		return
	}
	dispatcher := runtime.auditDispatcher
	if dispatcher == nil {
		return
	}
	if _, err := dispatcher.DispatchOnce(ctx); err != nil {
		s.auditHealth.recordFailure(time.Now())
		log.Printf("audit projection failed error=%v", err)
	}
	dispatcher.Notify()
}

func auditLifecyclePhase(action string) string {
	action = strings.TrimSpace(action)
	if index := strings.LastIndexByte(action, '.'); index >= 0 && index+1 < len(action) {
		switch phase := action[index+1:]; phase {
		case "requested", "approval_pending", "started", "running", "completed", "failed", "declined", "canceled", "stale", "expired", "blocked", "outcome_unknown", "updated", "created", "deleted", "archived", "closed", "connected", "connecting", "paused", "pending":
			return phase
		}
	}
	return "observed"
}

func auditConnectorMetadata(payload any) (string, int64, int64, int64, int64) {
	values, ok := payload.(map[string]any)
	if !ok {
		return "", 0, 0, 0, 0
	}
	connectorKind := strings.TrimSpace(fmt.Sprint(values["connector_kind"]))
	projectID := int64FromAny(values["project_id"])
	targetID := int64FromAny(values["target_id"])
	profileID := int64FromAny(values["profile_id"])
	actionRequestID := int64FromAny(values["action_request_id"])
	if actionRequestID == 0 {
		actionRequestID = int64FromAny(values["request_id"])
	}
	if (connectorKind == "" || targetID == 0 || profileID == 0) && values["target_ref"] != nil {
		kind, parsedTargetID, parsedProfileID, ok := connectors.ParseTargetRef(fmt.Sprint(values["target_ref"]))
		if ok {
			if connectorKind == "" {
				connectorKind = kind
			}
			if targetID == 0 {
				targetID = parsedTargetID
			}
			if profileID == 0 {
				profileID = parsedProfileID
			}
		}
	}
	return connectorKind, projectID, targetID, profileID, actionRequestID
}

func resolveAuditProjectID(ctx context.Context, executor sqldb.Executor, projectID int64, targetID int64, runtimeID int64) int64 {
	if projectID > 0 || executor == nil {
		return projectID
	}
	if targetID > 0 {
		_ = executor.QueryRowContext(ctx, `SELECT project_id FROM connector_targets WHERE id = ?`, targetID).Scan(&projectID)
		if projectID > 0 {
			return projectID
		}
	}
	if runtimeID > 0 {
		_ = executor.QueryRowContext(ctx, `
			SELECT ct.project_id
			FROM connector_runtime_surfaces rs
			JOIN connector_targets ct ON ct.id = rs.target_id
			WHERE rs.id = ?`, runtimeID).Scan(&projectID)
	}
	return projectID
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		var parsed int64
		if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err == nil {
			return parsed
		}
	}
	return 0
}

func int64Ptr(value int64) *int64 {
	return &value
}
