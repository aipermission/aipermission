package gatewayvault

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultactions"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type atomicActionStub struct {
	VaultActionApplication
}

func (atomicActionStub) PrepareTransactional(_ context.Context, request vaultrequests.Request) (vaultactions.TransactionalExecution, bool, error) {
	return vaultactions.TransactionalExecution{Run: func(ctx context.Context, tx *sql.Tx) (any, []vaultactions.TransactionalObservation, error) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO atomic_effects (request_id) VALUES (?)`, request.ID); err != nil {
			return nil, nil, err
		}
		return map[string]any{"created": true}, []vaultactions.TransactionalObservation{{Action: "vault.item.created", Payload: map[string]any{"request_id": request.ID}}}, nil
	}}, true, nil
}

func (atomicActionStub) IsStale(error) bool { return false }

func TestAtomicVaultEffectAndRequestCompletionCommitTogether(t *testing.T) {
	for _, test := range []struct {
		name            string
		failObservation bool
		wantErr         bool
		wantEffects     int
		wantStatus      string
	}{
		{name: "commit", wantEffects: 1, wantStatus: vaultrequests.StatusCompleted},
		{name: "rollback", failObservation: true, wantErr: true, wantStatus: vaultrequests.StatusRunning},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, request := atomicRequestFixture(t)
			observations := []string{}
			runtime := Runtime{Requests: RequestRuntimePorts{
				RedactRequestError: func(_ context.Context, err error) string { return err.Error() },
				Transaction: func(ctx context.Context, mutate func(*sql.Tx, RequestObservationAppender) error) error {
					tx, err := database.BeginTx(ctx, nil)
					if err != nil {
						return err
					}
					defer tx.Rollback()
					appendObservation := func(_ *sql.Tx, _ string, _ *int64, _ int64, action string, _ any) error {
						observations = append(observations, action)
						if test.failObservation && len(observations) == 2 {
							return errors.New("audit append failed")
						}
						return nil
					}
					if err := mutate(tx, appendObservation); err != nil {
						return err
					}
					return tx.Commit()
				},
			}}
			execute := requestMutationPort{component: &Component{}, runtime: runtime}.executeAtomic(atomicActionStub{})
			result, handled, err := execute(t.Context(), request, "mcp", "", "mcp.vault_action")
			if handled != true || (err != nil) != test.wantErr {
				t.Fatalf("handled=%v result=%#v err=%v", handled, result, err)
			}
			var effects int
			if err := database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM atomic_effects`).Scan(&effects); err != nil {
				t.Fatal(err)
			}
			persisted, err := vaultrequests.NewStore(database).Get(t.Context(), request.ID)
			if err != nil || effects != test.wantEffects || persisted.Status != test.wantStatus {
				t.Fatalf("effects=%d request=%#v err=%v", effects, persisted, err)
			}
		})
	}
}

func atomicRequestFixture(t *testing.T) (*sql.DB, vaultrequests.Request) {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "atomic-vault.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.ExecContext(t.Context(), `CREATE TABLE atomic_effects (request_id INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	project, err := projectstore.NewStore(database).Create(t.Context(), "Atomic Vault")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(database).Create(t.Context(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	request, created, err := vaultrequests.NewStore(database).Create(t.Context(), vaultrequests.CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: vaultrequests.ActionGenerateItem,
		Input: map[string]any{"name": "TOKEN", "generator_kind": "hex_32"}, Reason: "test atomic finalization",
		ApprovalContext: map[string]any{}, ApprovalContextHash: "hash", IdempotencyKey: "atomic-request",
		InitialStatus: vaultrequests.StatusRunning,
	})
	if err != nil || !created {
		t.Fatalf("create request: created=%v err=%v", created, err)
	}
	return database, request
}
