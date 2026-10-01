package backup

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type importZeroReader struct{}

func (importZeroReader) Read(data []byte) (int, error) {
	clear(data)
	return len(data), nil
}

func TestDatabaseImportSeparatesArtifactFromMultipartEnvelope(t *testing.T) {
	for _, test := range []struct {
		name         string
		bytes        int64
		envelope     int64
		wantStatus   int
		wantAttempts int
	}{
		{name: "exact artifact limit", bytes: backups.MaxDatabaseTransferBytes, wantStatus: http.StatusOK, wantAttempts: 1},
		{name: "one byte too large", bytes: backups.MaxDatabaseTransferBytes + 1, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "exact envelope limit", bytes: backups.MaxDatabaseTransferBytes, envelope: backups.MaxDatabaseTransferBytes + maxDatabaseMultipartOverhead, wantStatus: http.StatusOK, wantAttempts: 1},
		{name: "one byte oversized envelope", bytes: backups.MaxDatabaseTransferBytes, envelope: backups.MaxDatabaseTransferBytes + maxDatabaseMultipartOverhead + 1, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "empty artifact", wantStatus: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			temp := t.TempDir()
			t.Setenv("TMPDIR", temp)
			attempts, installed := 0, false
			lifecycle := newOperationOrderLifecycle(t)
			component := New(Dependencies{
				Lifecycle: importCommitLifecycle{Service: lifecycle, importFn: func(_ context.Context, input workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error) {
					installed = true
					path := filepath.Join(t.TempDir(), "candidate.aipdb")
					if err := input.Write(path); err != nil {
						return workspacelifecycle.Transition{}, err
					}
					info, err := os.Stat(path)
					if err != nil || info.Size() != test.bytes {
						t.Fatalf("artifact size changed: info=%v err=%v", info, err)
					}
					if err := input.BeforeCommit(); err != nil {
						return workspacelifecycle.Transition{}, err
					}
					if err := input.BeforePublish(); err != nil {
						return workspacelifecycle.Transition{}, err
					}
					return workspacelifecycle.Transition{Identity: workspacelifecycle.Identity{ID: "imported"}}, nil
				}},
				AcquireOperation: (&OperationLimiter{}).Acquire,
				AuthorizeImport:  func(http.ResponseWriter, *http.Request) (func() bool, bool) { return func() bool { return true }, true },
				BeginAttempt: func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool) {
					attempts++
					return &importPasswordAttempt{}, true
				},
				IssuePrepared: func(http.ResponseWriter, uisession.Prepared) error { return nil },
			})
			request, stop := databaseImportSizeRequest(t, test.bytes, test.envelope)
			defer stop()
			response := httptest.NewRecorder()
			component.HTTPHandlers().Import(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if installed != (test.wantStatus == http.StatusOK) || attempts != test.wantAttempts {
				t.Fatalf("invalid artifact reached installation/password attempt: installed=%v attempts=%d", installed, attempts)
			}
			entries, err := os.ReadDir(temp)
			if err != nil || len(entries) != 0 {
				t.Fatalf("multipart staging leaked: entries=%v err=%v", entries, err)
			}
		})
	}
}

func databaseImportSizeRequest(t *testing.T, size, envelope int64) (*http.Request, func()) {
	t.Helper()
	var metadata bytes.Buffer
	contentType, err := writeDatabaseImportSizeForm(&metadata, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var padding int64
	if envelope > 0 {
		padding = envelope - size - int64(metadata.Len())
		if padding < 0 {
			t.Fatal("fixture envelope cannot contain the requested artifact")
		}
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := writeDatabaseImportSizeForm(writer, size, padding)
		_ = writer.CloseWithError(err)
		done <- err
	}()
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", reader)
	request.Header.Set("Content-Type", contentType)
	return request, func() {
		_ = reader.Close()
		_ = writer.Close()
		<-done
	}
}

// One encoder calculates exact envelope overhead and emits the streamed body.
func writeDatabaseImportSizeForm(output io.Writer, size, padding int64) (string, error) {
	form := multipart.NewWriter(output)
	if err := form.SetBoundary("import-size-fixture"); err != nil {
		return "", err
	}
	if err := form.WriteField("database_name", "Import Copy"); err != nil {
		return "", err
	}
	if err := form.WriteField("database_password", "FixturePassword123"); err != nil {
		return "", err
	}
	file, err := form.CreateFormFile("sqlite", "fixture.aipdb")
	if err != nil {
		return "", err
	}
	if _, err := io.CopyN(file, importZeroReader{}, size); err != nil {
		return "", err
	}
	field, err := form.CreateFormField("padding")
	if err != nil {
		return "", err
	}
	if _, err := io.CopyN(field, importZeroReader{}, padding); err != nil {
		return "", err
	}
	return form.FormDataContentType(), form.Close()
}
