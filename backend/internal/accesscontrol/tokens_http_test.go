package accesscontrol

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestCreateTokenRequiresOnlyItsDeclaredCapabilities(t *testing.T) {
	database := openTestDatabase(t)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Tokens: tokens.NewStore(database),
			ReusableTokensForMutation: func(context.Context, *sql.Tx) (bool, error) {
				return false, nil
			},
			Mutate: auditRunner(database, nil),
		}, true
	})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tokens", handlers.CreateToken)
	request := httptest.NewRequest(http.MethodPost, "/tokens", bytes.NewBufferString(`{"name":"agent"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("create token: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store, private" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("sensitive response headers = %#v", response.Header())
	}
	if countRows(t, database, "api_tokens") != 1 || countRows(t, database, "audit_logs") != 1 {
		t.Fatal("token and audit were not committed together")
	}
}

func TestCreateTokenFailsClosedWithoutMutationRunner(t *testing.T) {
	database := openTestDatabase(t)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Tokens: tokens.NewStore(database),
			ReusableTokensForMutation: func(context.Context, *sql.Tx) (bool, error) {
				return false, nil
			},
		}, true
	})
	request := httptest.NewRequest(http.MethodPost, "/tokens", bytes.NewBufferString(`{"name":"agent"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handlers.CreateToken(response, request)

	if response.Code != http.StatusInternalServerError || countRows(t, database, "api_tokens") != 0 {
		t.Fatalf("missing mutation runner: %d %s", response.Code, response.Body.String())
	}
}

func TestCreateTokenReadsReusablePolicyInsideItsMutationTransaction(t *testing.T) {
	database := openTestDatabase(t)
	secretVault, err := vault.New("token-setting-race-test-key")
	if err != nil {
		t.Fatal(err)
	}
	store := tokens.NewEncryptedStore(database, secretVault, "token-setting-race-workspace")
	policy := securitypolicy.NewService(database)
	mutate := auditRunner(database, nil)
	if _, err := policy.UpdateSettings(t.Context(), securitypolicy.Settings{ReusableTokens: true}, auditedmutation.Runner(mutate)); err != nil {
		t.Fatal(err)
	}

	mutationStarted := make(chan struct{})
	resumeMutation := make(chan struct{})
	requestDone := make(chan struct{})
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Tokens: store, ReusableTokensForMutation: policy.ReusableTokensForMutation,
			Mutate: func(ctx context.Context, action string, payload func() any, callback func(*sql.Tx) error) error {
				close(mutationStarted)
				select {
				case <-resumeMutation:
				case <-ctx.Done():
					return ctx.Err()
				}
				return mutate(ctx, action, payload, callback)
			},
		}, true
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request := httptest.NewRequest(http.MethodPost, "/tokens", strings.NewReader(`{"name":"race-agent"}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	go func() {
		defer close(requestDone)
		handlers.CreateToken(response, request)
	}()
	select {
	case <-mutationStarted:
	case <-ctx.Done():
		t.Fatal("token creation did not reach its mutation boundary")
	}
	if _, err := policy.UpdateSettings(ctx, securitypolicy.Settings{ReusableTokens: false}, auditedmutation.Runner(mutate)); err != nil {
		close(resumeMutation)
		<-requestDone
		t.Fatal(err)
	}
	close(resumeMutation)
	select {
	case <-requestDone:
	case <-ctx.Done():
		t.Fatal("token creation did not finish")
	}
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"token":"aip_`) {
		t.Fatalf("create token: %d %s", response.Code, response.Body.String())
	}
	assertNoReusableTokenMaterial(t, database, store)

	if _, err := policy.UpdateSettings(ctx, securitypolicy.Settings{ReusableTokens: true}, auditedmutation.Runner(mutate)); err != nil {
		t.Fatal(err)
	}
	assertNoReusableTokenMaterial(t, database, store)
}

func TestCreateTokenAndReusablePolicyUpdateRemainOrderedAndAtomic(t *testing.T) {
	database := openTestDatabase(t)
	secretVault, err := vault.New("token-setting-order-test-key")
	if err != nil {
		t.Fatal(err)
	}
	store := tokens.NewEncryptedStore(database, secretVault, "token-setting-order-workspace")
	policy := securitypolicy.NewService(database)
	mutate := auditRunner(database, nil)
	if _, err := policy.UpdateSettings(t.Context(), securitypolicy.Settings{ReusableTokens: true}, auditedmutation.Runner(mutate)); err != nil {
		t.Fatal(err)
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Tokens: store, ReusableTokensForMutation: policy.ReusableTokensForMutation,
			Mutate: mutate,
		}, true
	})
	request := httptest.NewRequest(http.MethodPost, "/tokens", strings.NewReader(`{"name":"ordered-agent"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handlers.CreateToken(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create token: %d %s", response.Code, response.Body.String())
	}
	if _, err := policy.UpdateSettings(t.Context(), securitypolicy.Settings{ReusableTokens: false}, auditedmutation.Runner(mutate)); err != nil {
		t.Fatal(err)
	}
	assertNoReusableTokenMaterial(t, database, store)
}

func TestCreateTokenAuditFailureRollsBackReusableCiphertext(t *testing.T) {
	database := openTestDatabase(t)
	secretVault, err := vault.New("token-setting-rollback-test-key")
	if err != nil {
		t.Fatal(err)
	}
	store := tokens.NewEncryptedStore(database, secretVault, "token-setting-rollback-workspace")
	policy := securitypolicy.NewService(database)
	baselineMutation := auditRunner(database, nil)
	if _, err := policy.UpdateSettings(t.Context(), securitypolicy.Settings{ReusableTokens: true}, auditedmutation.Runner(baselineMutation)); err != nil {
		t.Fatal(err)
	}
	forced := errors.New("forced audit failure")
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Tokens: store, ReusableTokensForMutation: policy.ReusableTokensForMutation,
			Mutate: auditRunner(database, forced),
		}, true
	})
	request := httptest.NewRequest(http.MethodPost, "/tokens", strings.NewReader(`{"name":"rollback-agent"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handlers.CreateToken(response, request)
	if response.Code != http.StatusInternalServerError || countRows(t, database, "api_tokens") != 0 {
		t.Fatalf("failed mutation persisted token: %d %s", response.Code, response.Body.String())
	}
}

func assertNoReusableTokenMaterial(t *testing.T, database *sql.DB, store *tokens.Store) {
	t.Helper()
	var retained int
	if err := database.QueryRow(`SELECT COUNT(*) FROM api_tokens WHERE token_value <> ''`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.TokenValue != "" {
			t.Fatalf("token %d retained reusable material", item.ID)
		}
	}
	if retained != 0 {
		t.Fatalf("retained reusable token rows = %d", retained)
	}
}
