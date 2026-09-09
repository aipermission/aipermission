package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/auditoutbox"
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
	event, err := auditoutbox.BuildEvent(ctx, runtime.database, auditoutbox.BuildInput{
		ActorType: actorType,
		TokenID:   tokenID,
		RuntimeID: runtimeID,
		Action:    action,
		Payload:   payload,
		Redact:    s.prepareAuditRedactor(ctx, runtime),
	})
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
		event, err := auditoutbox.BuildEvent(ctx, tx, auditoutbox.BuildInput{
			ActorType: actorType,
			TokenID:   tokenID,
			RuntimeID: runtimeID,
			Action:    action,
			Payload:   payload,
			Redact:    redact,
		})
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

func int64Ptr(value int64) *int64 {
	return &value
}
