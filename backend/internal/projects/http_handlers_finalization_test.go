package projects

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/vaultfinalization"
)

func TestArchiveReportsCommittedChangeWhenInvalidationIsPending(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "project.aipdb"), "ProjectFinalizationPassword123")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	project, err := NewStore(database).Create(t.Context(), "Archive Me")
	if err != nil {
		t.Fatal(err)
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Database: database,
			Mutate: func(ctx context.Context, _ string, _ func() any, mutate func(*sql.Tx) error) error {
				tx, err := database.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				defer tx.Rollback()
				if err := mutate(tx); err != nil {
					return err
				}
				return tx.Commit()
			},
			AcquireExclusive: func(context.Context) (func(), error) { return func() {}, nil },
			Invalidate: func(context.Context, int64, []SessionReference) error {
				return errors.New("injected project invalidation failure")
			},
		}, true
	})
	request := httptest.NewRequest(http.MethodDelete, "/api/projects/"+strconv.FormatInt(project.ID, 10), nil)
	request.SetPathValue("id", strconv.FormatInt(project.ID, 10))
	response := httptest.NewRecorder()
	handlers.Archive(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "vault_finalization_pending") {
		t.Fatalf("archive response = %d %s", response.Code, response.Body.String())
	}
	var status string
	if err := database.QueryRowContext(t.Context(), `SELECT status FROM projects WHERE id = ?`, project.ID).Scan(&status); err != nil || status != "archived" {
		t.Fatalf("project status = %q, error = %v", status, err)
	}
	if err := vaultfinalization.NewStore(database).RequireReady(t.Context()); !errors.Is(err, vaultfinalization.ErrBlocked) {
		t.Fatalf("readiness = %v", err)
	}
}
