package connectormanagement

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestProfileMutationHandlersOwnCreateAndUpdateTransactions(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	auditActions := []string{}
	ensured := []int64{}
	beforeCreate := 0
	acquired := 0
	released := 0
	lifecycleChanges := []TargetLifecycleChange{}
	scope := profileMutationTestScope(fixture, &auditActions)
	scope.Preparation = CredentialPreparationPorts{
		Decrypt: func(_ context.Context, profileID int64, encrypted string) (map[string]any, error) {
			if profileID < 1 || encrypted != "encrypted-profile-secret" {
				t.Fatalf("decrypt input = %d %q", profileID, encrypted)
			}
			return map[string]any{}, nil
		},
		Encrypt: func(_ context.Context, profileID int64, secret map[string]any) (string, error) {
			if profileID < 1 || secret == nil {
				t.Fatalf("encrypt input = %d %#v", profileID, secret)
			}
			return "encrypted-profile-secret", nil
		},
	}
	scope.BeforeCreate = func(_ context.Context, target connectortargets.Target) error {
		beforeCreate++
		if target.ID != fixture.target.ID {
			t.Fatalf("before-create target = %d", target.ID)
		}
		return nil
	}
	scope.EnsureRuntimeSurfaces = func(_ context.Context, _ *connectortargets.Store, _ connectortargets.Target, profile connectortargets.CredentialProfile) error {
		ensured = append(ensured, profile.ID)
		return nil
	}
	scope.AcquireExclusive = func(context.Context) (func(), error) {
		acquired++
		return func() { released++ }, nil
	}
	scope.AfterLifecycleChange = func(_ context.Context, change TargetLifecycleChange) error {
		lifecycleChanges = append(lifecycleChanges, change)
		return nil
	}
	handler := NewProfileMutationHTTPHandler(func(http.ResponseWriter) (ProfileMutationScope, bool) { return scope, true })
	mux := http.NewServeMux()
	mux.HandleFunc("POST /connector-targets/{id}/profiles", handler.Create)
	mux.HandleFunc("PUT /connector-targets/{id}/profiles/{profile_id}", handler.Update)

	create := performProfileMutationJSON(t, mux, http.MethodPost,
		"/connector-targets/"+strconv.FormatInt(fixture.target.ID, 10)+"/profiles",
		CredentialProfileInput{Kind: "operator", Label: "secondary", Secret: map[string]any{}},
	)
	var created ProfileSummary
	decodeManagementResponse(t, create, &created)
	if create.Code != http.StatusCreated || created.ID < 1 || strings.Contains(create.Body.String(), "encrypted-profile-secret") {
		t.Fatalf("create profile: %d %s", create.Code, create.Body.String())
	}
	persisted, err := connectortargets.NewStore(fixture.database).GetCredentialProfile(t.Context(), fixture.target.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.EncryptedSecretJSON != "encrypted-profile-secret" || beforeCreate != 1 {
		t.Fatalf("persisted=%#v beforeCreate=%d", persisted, beforeCreate)
	}

	update := performProfileMutationJSON(t, mux, http.MethodPut,
		"/connector-targets/"+strconv.FormatInt(fixture.target.ID, 10)+"/profiles/"+strconv.FormatInt(created.ID, 10),
		CredentialProfileInput{Kind: "operator", Label: "secondary-renamed"},
	)
	var updated ProfileSummary
	decodeManagementResponse(t, update, &updated)
	if update.Code != http.StatusOK || updated.Label != "secondary-renamed" {
		t.Fatalf("update profile: %d %s", update.Code, update.Body.String())
	}
	after, err := connectortargets.NewStore(fixture.database).GetCredentialProfile(t.Context(), fixture.target.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.EncryptedSecretJSON != persisted.EncryptedSecretJSON || after.SecretRevision != persisted.SecretRevision {
		t.Fatal("metadata-only update rewrote the credential secret")
	}
	if acquired != 2 || released != 2 || len(ensured) != 2 || len(lifecycleChanges) != 1 || lifecycleChanges[0].ProfileID != created.ID {
		t.Fatalf("acquired=%d released=%d ensured=%v lifecycle=%#v", acquired, released, ensured, lifecycleChanges)
	}
	if strings.Join(auditActions, ",") != "connector.profile.created,connector.profile.updated" {
		t.Fatalf("audit actions = %v", auditActions)
	}
}

func TestProfileCreateRollsBackWhenSecretEncryptionFails(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	auditActions := []string{}
	scope := profileMutationTestScope(fixture, &auditActions)
	scope.Preparation.Encrypt = func(context.Context, int64, map[string]any) (string, error) {
		return "", errors.New("fixture encryption failure")
	}
	handler := NewProfileMutationHTTPHandler(func(http.ResponseWriter) (ProfileMutationScope, bool) { return scope, true })
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/connector-targets/1/profiles", strings.NewReader(`{"kind":"operator","label":"must-rollback","secret":{}}`))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", strconv.FormatInt(fixture.target.ID, 10))
	handler.Create(response, request)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "fixture") {
		t.Fatalf("encryption failure response = %d %s", response.Code, response.Body.String())
	}
	profiles, err := connectortargets.NewStore(fixture.database).ListCredentialProfiles(t.Context(), fixture.target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || len(auditActions) != 0 {
		t.Fatalf("failed create escaped transaction: profiles=%d audit=%v", len(profiles), auditActions)
	}
}

func TestProfileMutationHandlersFailClosedForIncompleteScopes(t *testing.T) {
	handler := NewProfileMutationHTTPHandler(func(http.ResponseWriter) (ProfileMutationScope, bool) {
		return ProfileMutationScope{}, true
	})
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{name: "create", handler: handler.Create, path: "/connector-targets/1/profiles"},
		{name: "update", handler: handler.Update, path: "/connector-targets/1/profiles/1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			test.handler(response, httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(`{}`)))
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestProfileMutationsRejectMissingExclusiveRelease(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	for _, testCase := range []struct {
		name, method, path string
		payload            CredentialProfileInput
	}{
		{name: "create", method: http.MethodPost, path: "/connector-targets/" + strconv.FormatInt(fixture.target.ID, 10) + "/profiles", payload: CredentialProfileInput{Kind: "operator", Label: "must-not-create", Secret: map[string]any{}}},
		{name: "update", method: http.MethodPut, path: "/connector-targets/" + strconv.FormatInt(fixture.target.ID, 10) + "/profiles/" + strconv.FormatInt(fixture.profile.ID, 10), payload: CredentialProfileInput{Kind: fixture.profile.Kind, Label: "must-not-change"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			auditActions := []string{}
			scope := profileMutationTestScope(fixture, &auditActions)
			scope.AcquireExclusive = func(context.Context) (func(), error) { return nil, nil }
			handler := NewProfileMutationHTTPHandler(func(http.ResponseWriter) (ProfileMutationScope, bool) { return scope, true })
			mux := http.NewServeMux()
			pattern := map[string]string{
				"create": "POST /connector-targets/{id}/profiles",
				"update": "PUT /connector-targets/{id}/profiles/{profile_id}",
			}[testCase.name]
			mux.HandleFunc(pattern, map[string]http.HandlerFunc{"create": handler.Create, "update": handler.Update}[testCase.name])
			response := performProfileMutationJSON(t, mux, testCase.method, testCase.path, testCase.payload)
			if response.Code != http.StatusInternalServerError || len(auditActions) != 0 {
				t.Fatalf("missing release response=%d audit=%v: %s", response.Code, auditActions, response.Body.String())
			}
		})
	}
	profiles, err := connectortargets.NewStore(fixture.database).ListCredentialProfiles(t.Context(), fixture.target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Label != fixture.profile.Label {
		t.Fatalf("profiles changed without an exclusive release: %#v", profiles)
	}
}

func TestCredentialPreparationHTTPErrorClassification(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		err        error
		status     int
		want       string
		mustNotSee string
	}{
		{
			name: "input", err: CredentialInputError{Err: errors.New("invalid profile input")},
			status: http.StatusBadRequest, want: "invalid profile input",
		},
		{
			name: "secret decode", err: errors.Join(ErrCredentialSecretDecode, errors.New("cipher detail")),
			status: http.StatusInternalServerError, mustNotSee: "cipher detail",
		},
		{
			name: "target validation", err: connectortargets.ValidationError("invalid target field"),
			status: http.StatusBadRequest, want: "invalid target field",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeCredentialPreparationError(response, testCase.err)
			if response.Code != testCase.status ||
				(testCase.want != "" && !strings.Contains(response.Body.String(), testCase.want)) ||
				(testCase.mustNotSee != "" && strings.Contains(response.Body.String(), testCase.mustNotSee)) {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
		})
	}
}

func profileMutationTestScope(fixture *managementHTTPFixture, auditActions *[]string) ProfileMutationScope {
	return ProfileMutationScope{
		Database: fixture.database,
		Registry: fixture.registry,
		Preparation: CredentialPreparationPorts{
			Encrypt: func(context.Context, int64, map[string]any) (string, error) { return "encrypted", nil },
		},
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
			tx, err := fixture.database.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			appendAudit := func(_ *sql.Tx, _ string, _ *int64, _ int64, action string, _ any) error {
				*auditActions = append(*auditActions, action)
				return nil
			}
			if err := mutate(tx, appendAudit); err != nil {
				return err
			}
			return tx.Commit()
		},
		BeforeCreate: func(context.Context, connectortargets.Target) error { return nil },
		EnsureRuntimeSurfaces: func(context.Context, *connectortargets.Store, connectortargets.Target, connectortargets.CredentialProfile) error {
			return nil
		},
		AcquireExclusive:     func(context.Context) (func(), error) { return func() {}, nil },
		AfterLifecycleChange: func(context.Context, TargetLifecycleChange) error { return nil },
	}
}

func performProfileMutationJSON(t *testing.T, handler http.Handler, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
