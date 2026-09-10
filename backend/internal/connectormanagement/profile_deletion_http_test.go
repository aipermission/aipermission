package connectormanagement

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestProfileDeletionOwnsCleanupAuditAndInvalidationOrder(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	steps := []string{}
	var auditPayload map[string]any
	scope := ProfileDeletionScope{
		Database: fixture.database,
		AcquireExclusive: func(context.Context) (func(), error) {
			steps = append(steps, "acquire")
			return func() { steps = append(steps, "release") }, nil
		},
		Cleanup: func(_ context.Context, target connectortargets.Target, profile connectortargets.CredentialProfile) (ProfileCleanupOutcome, error) {
			steps = append(steps, "cleanup")
			if target.ID != fixture.target.ID || profile.ID != fixture.profile.ID {
				t.Fatalf("cleanup identity = %d/%d", target.ID, profile.ID)
			}
			return ProfileCleanupOutcome{Required: true, Status: "completed", Output: map[string]any{"removed": true}}, nil
		},
		BeforeDelete: func(context.Context, connectortargets.Target, connectortargets.CredentialProfile) error {
			steps = append(steps, "before-delete")
			return nil
		},
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
			steps = append(steps, "transaction")
			tx, err := fixture.database.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if err := mutate(tx, func(_ *sql.Tx, _ string, _ *int64, _ int64, action string, payload any) error {
				steps = append(steps, "audit:"+action)
				auditPayload, _ = payload.(map[string]any)
				return nil
			}); err != nil {
				return err
			}
			return tx.Commit()
		},
		AfterLifecycleChange: func(_ context.Context, change TargetLifecycleChange) error {
			steps = append(steps, "invalidate")
			if change.ProfileID != fixture.profile.ID || !change.IncludeRunning {
				t.Fatalf("lifecycle change = %#v", change)
			}
			return nil
		},
	}
	handler := NewProfileDeletionHTTPHandler(func(http.ResponseWriter) (ProfileDeletionScope, bool) { return scope, true })
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /connector-targets/{id}/profiles/{profile_id}", handler.Delete)
	request := httptest.NewRequest(http.MethodDelete,
		"/connector-targets/"+strconv.FormatInt(fixture.target.ID, 10)+"/profiles/"+strconv.FormatInt(fixture.profile.ID, 10), nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete response = %d: %s", response.Code, response.Body.String())
	}
	wantSteps := []string{"acquire", "cleanup", "before-delete", "transaction", "audit:connector.profile.deleted", "invalidate", "release"}
	if len(steps) != len(wantSteps) {
		t.Fatalf("steps = %v", steps)
	}
	for index := range wantSteps {
		if steps[index] != wantSteps[index] {
			t.Fatalf("steps = %v", steps)
		}
	}
	cleanup, ok := auditPayload["external_cleanup"].(map[string]any)
	if !ok || cleanup["status"] != "completed" {
		t.Fatalf("audit payload = %#v", auditPayload)
	}
	if _, err := connectortargets.NewStore(fixture.database).GetCredentialProfile(t.Context(), fixture.target.ID, fixture.profile.ID); !errors.Is(err, connectortargets.ErrTargetProfileNotFound) {
		t.Fatalf("deleted profile lookup = %v", err)
	}
}

func TestProfileDeletionStopsBeforeMutationWhenCleanupFails(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	mutated := false
	scope := ProfileDeletionScope{
		Database:         fixture.database,
		AcquireExclusive: func(context.Context) (func(), error) { return func() {}, nil },
		Cleanup: func(context.Context, connectortargets.Target, connectortargets.CredentialProfile) (ProfileCleanupOutcome, error) {
			return ProfileCleanupOutcome{}, connectortargets.ValidationError("cleanup refused")
		},
		BeforeDelete: func(context.Context, connectortargets.Target, connectortargets.CredentialProfile) error { return nil },
		WithTransaction: func(context.Context, func(*sql.Tx, AuditAppender) error) error {
			mutated = true
			return nil
		},
		AfterLifecycleChange: func(context.Context, TargetLifecycleChange) error { return nil },
	}
	handler := NewProfileDeletionHTTPHandler(func(http.ResponseWriter) (ProfileDeletionScope, bool) { return scope, true })
	request := httptest.NewRequest(http.MethodDelete, "/", nil)
	request.SetPathValue("id", strconv.FormatInt(fixture.target.ID, 10))
	request.SetPathValue("profile_id", strconv.FormatInt(fixture.profile.ID, 10))
	response := httptest.NewRecorder()
	handler.Delete(response, request)
	if response.Code != http.StatusBadRequest || mutated {
		t.Fatalf("cleanup failure response=%d mutated=%v: %s", response.Code, mutated, response.Body.String())
	}
	if _, err := connectortargets.NewStore(fixture.database).GetCredentialProfile(t.Context(), fixture.target.ID, fixture.profile.ID); err != nil {
		t.Fatalf("profile removed after cleanup failure: %v", err)
	}
}

func TestProfileDeletionFailsClosedForMissingRuntime(t *testing.T) {
	handler := NewProfileDeletionHTTPHandler(func(http.ResponseWriter) (ProfileDeletionScope, bool) {
		return ProfileDeletionScope{}, true
	})
	response := httptest.NewRecorder()
	handler.Delete(response, httptest.NewRequest(http.MethodDelete, "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
}

func TestProfileDeletionRejectsMissingExclusiveRelease(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	cleanupCalled := false
	scope := ProfileDeletionScope{
		Database:         fixture.database,
		AcquireExclusive: func(context.Context) (func(), error) { return nil, nil },
		Cleanup: func(context.Context, connectortargets.Target, connectortargets.CredentialProfile) (ProfileCleanupOutcome, error) {
			cleanupCalled = true
			return ProfileCleanupOutcome{}, nil
		},
		BeforeDelete:         func(context.Context, connectortargets.Target, connectortargets.CredentialProfile) error { return nil },
		WithTransaction:      func(context.Context, func(*sql.Tx, AuditAppender) error) error { return nil },
		AfterLifecycleChange: func(context.Context, TargetLifecycleChange) error { return nil },
	}
	handler := NewProfileDeletionHTTPHandler(func(http.ResponseWriter) (ProfileDeletionScope, bool) { return scope, true })
	request := httptest.NewRequest(http.MethodDelete, "/", nil)
	request.SetPathValue("id", strconv.FormatInt(fixture.target.ID, 10))
	request.SetPathValue("profile_id", strconv.FormatInt(fixture.profile.ID, 10))
	response := httptest.NewRecorder()
	handler.Delete(response, request)
	if response.Code != http.StatusInternalServerError || cleanupCalled {
		t.Fatalf("missing release response=%d cleanup=%v: %s", response.Code, cleanupCalled, response.Body.String())
	}
}
