package connectormanagement

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestCombinedMutationHandlersOwnAtomicCreateAndUpdate(t *testing.T) {
	fixture := newManagementHTTPFixture(t)
	audits := []string{}
	ensured := []int64{}
	lifecycle := []TargetLifecycleChange{}
	scope := CombinedMutationScope{
		Database: fixture.database,
		Registry: fixture.registry,
		Preparation: CredentialPreparationPorts{
			Decrypt: func(context.Context, int64, string) (map[string]any, error) { return map[string]any{}, nil },
			Encrypt: func(context.Context, int64, map[string]any) (string, error) { return "combined-ciphertext", nil },
		},
		ValidateTransport: func(_ context.Context, projectID int64, config map[string]any) error {
			if projectID < 1 || config["endpoint"] == "" {
				return connectortargets.ValidationError("invalid transport")
			}
			return nil
		},
		AcquireExclusive: func(context.Context) (func(), error) { return func() {}, nil },
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
			tx, err := fixture.database.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if err := mutate(tx, func(_ *sql.Tx, _ string, _ *int64, _ int64, action string, _ any) error {
				audits = append(audits, action)
				return nil
			}); err != nil {
				return err
			}
			return tx.Commit()
		},
		BeforeCreate: func(context.Context, connectortargets.Target) error { return nil },
		EnsureRuntimeSurfaces: func(_ context.Context, _ *connectortargets.Store, _ connectortargets.Target, profile connectortargets.CredentialProfile) error {
			ensured = append(ensured, profile.ID)
			return nil
		},
		AfterLifecycleChange: func(_ context.Context, change TargetLifecycleChange) error {
			lifecycle = append(lifecycle, change)
			return nil
		},
	}
	handler := NewCombinedMutationHTTPHandler(func(http.ResponseWriter) (CombinedMutationScope, bool) { return scope, true })
	mux := http.NewServeMux()
	mux.HandleFunc("POST /connector-targets/with-profile", handler.Create)
	mux.HandleFunc("PUT /connector-targets/{id}/with-profile/{profile_id}", handler.Update)

	create := performProfileMutationJSON(t, mux, http.MethodPost, "/connector-targets/with-profile", CreateTargetWithProfileRequest{
		Target:  CreateTargetRequest{ProjectID: fixture.target.ProjectID, ConnectorKind: managementTestConnectorKind, Name: "Combined", Config: map[string]any{"endpoint": "combined"}},
		Profile: CredentialProfileInput{Kind: "operator", Label: "combined", Public: map[string]any{"managed_marker": "keep"}, Secret: map[string]any{}},
	})
	var created TargetResponse
	decodeManagementResponse(t, create, &created)
	if create.Code != http.StatusCreated || created.ID < 1 || len(created.Profiles) != 1 {
		t.Fatalf("combined create = %d: %s", create.Code, create.Body.String())
	}
	profileID := created.Profiles[0].ID

	update := performProfileMutationJSON(t, mux, http.MethodPut,
		"/connector-targets/"+strconv.FormatInt(created.ID, 10)+"/with-profile/"+strconv.FormatInt(profileID, 10),
		UpdateTargetWithProfileRequest{
			Target:  UpdateTargetRequest{Name: "Combined renamed", Config: map[string]any{"endpoint": "combined-updated"}},
			Profile: CredentialProfileInput{Kind: "operator", Label: "combined-renamed"},
		},
	)
	var updated TargetResponse
	decodeManagementResponse(t, update, &updated)
	if update.Code != http.StatusOK || updated.Name != "Combined renamed" || len(updated.Profiles) != 1 {
		t.Fatalf("combined update = %d: %s", update.Code, update.Body.String())
	}
	profile, err := connectortargets.NewStore(fixture.database).GetCredentialProfile(t.Context(), created.ID, profileID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Public["managed_marker"] != "keep" || profile.EncryptedSecretJSON != "combined-ciphertext" {
		t.Fatalf("updated profile = %#v", profile)
	}
	wantAudits := "connector.target.created,connector.profile.created,connector.target.updated,connector.profile.updated"
	if strings.Join(audits, ",") != wantAudits || len(ensured) != 2 || len(lifecycle) != 1 || lifecycle[0].ProfileID != 0 {
		t.Fatalf("audits=%v ensured=%v lifecycle=%#v", audits, ensured, lifecycle)
	}
}

func TestCombinedMutationHandlersFailClosedForIncompleteScope(t *testing.T) {
	handler := NewCombinedMutationHTTPHandler(func(http.ResponseWriter) (CombinedMutationScope, bool) {
		return CombinedMutationScope{}, true
	})
	response := performProfileMutationJSON(t, http.HandlerFunc(handler.Create), http.MethodPost, "/", CreateTargetWithProfileRequest{})
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
}
