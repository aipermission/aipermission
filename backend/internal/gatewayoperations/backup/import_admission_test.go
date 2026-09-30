package backup

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type importCommitLifecycle struct {
	*workspacelifecycle.Service[*operationOrderRuntime]
	importFn func(context.Context, workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error)
}

func (lifecycle importCommitLifecycle) Import(ctx context.Context, input workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error) {
	return lifecycle.importFn(ctx, input)
}

type importPasswordAttempt struct{ succeeded bool }

func (attempt *importPasswordAttempt) Success() { attempt.succeeded = true }
func (*importPasswordAttempt) Failure()         {}

func TestImportRetainsCommitLeaseThroughSessionIssuance(t *testing.T) {
	lifecycle := newOperationOrderLifecycle(t)
	attempt := &importPasswordAttempt{}
	validated, issued := false, false
	component := New(Dependencies{
		Lifecycle: importCommitLifecycle{Service: lifecycle, importFn: func(ctx context.Context, input workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error) {
			if err := input.Write(filepath.Join(t.TempDir(), "candidate.aipdb")); err != nil {
				return workspacelifecycle.Transition{}, err
			}
			if err := input.BeforeCommit(); err != nil {
				return workspacelifecycle.Transition{}, err
			}
			if err := input.BeforePublish(); err != nil {
				return workspacelifecycle.Transition{}, err
			}
			return workspacelifecycle.Transition{Identity: workspacelifecycle.Identity{ID: "import-copy"}}, nil
		}},
		AcquireOperation: (&OperationLimiter{}).Acquire,
		AuthorizeImport: func(http.ResponseWriter, *http.Request) (func() bool, bool) {
			return func() bool { validated = true; return true }, true
		},
		BeginAttempt: func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool) { return attempt, true },
		IssuePrepared: func(http.ResponseWriter, uisession.Prepared) error {
			issued = true
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			release, err := lifecycle.AcquireReadContext(ctx)
			if release != nil {
				release()
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("commit writer was released before issuing the imported session")
			}
			return nil
		},
	})
	request := importAdmissionRequest(t)
	response := httptest.NewRecorder()
	component.HTTPHandlers().Import(response, request)
	if response.Code != http.StatusOK || !validated || !issued || !attempt.succeeded {
		t.Fatalf("import status=%d validated=%t issued=%t succeeded=%t", response.Code, validated, issued, attempt.succeeded)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	release, err := lifecycle.AcquireMutationContext(ctx)
	if err != nil {
		t.Fatal("import retained its writer after responding")
	}
	release()
}

func importAdmissionRequest(t *testing.T) *http.Request {
	t.Helper()
	var encoded bytes.Buffer
	form := multipart.NewWriter(&encoded)
	if err := form.WriteField("database_name", "Import Copy"); err != nil {
		t.Fatal(err)
	}
	if err := form.WriteField("database_password", "FixturePassword123"); err != nil {
		t.Fatal(err)
	}
	file, err := form.CreateFormFile("sqlite", "fixture.aipdb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("fixture")); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", &encoded)
	request.Header.Set("Content-Type", form.FormDataContentType())
	return request
}

func TestImportAdmissionFailsClosedWithoutAuthorizer(t *testing.T) {
	lifecycle := newOperationOrderLifecycle(t)
	component := New(Dependencies{Lifecycle: lifecycle, AcquireOperation: (&OperationLimiter{}).Acquire})
	response := httptest.NewRecorder()
	component.HTTPHandlers().Import(response, importAdmissionRequest(t))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("missing import authorizer status=%d", response.Code)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	release, err := lifecycle.AcquireMutationContext(ctx)
	if err != nil {
		t.Fatal("failed import admission leaked its lifecycle lease")
	}
	release()
}
