package backups

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/backups/serviceboundary"
)

const numericServiceToken = "12345678901234567890123456789012"

type decoderErrorBody struct{ cause error }

func (body decoderErrorBody) Read([]byte) (int, error) {
	return 0, errors.Join(errors.New("remote read failure: "+numericServiceToken), body.cause)
}

func TestServiceDecoderKeepsSuccessfulCredentialBoundary(t *testing.T) {
	for _, name := range []string{"My Project", numericServiceToken, "Bearer " + numericServiceToken, base64.StdEncoding.EncodeToString([]byte(numericServiceToken))} {
		t.Run(name, func(t *testing.T) {
			client, err := NewServiceClient("http://127.0.0.1:8080", numericServiceToken)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(servicePage[ServiceStream]{Items: []ServiceStream{{
				ID: "stream-a", DatabaseName: name, CreatedAt: "2026-10-04T12:00:00Z", UpdatedAt: "2026-10-04T12:00:00Z", BackupCount: 42,
			}}})
			if err != nil {
				t.Fatal(err)
			}
			client.client.Transport = &decoderResponseTransport{body: io.NopCloser(strings.NewReader(string(payload)))}
			streams, err := client.ListStreams(t.Context())
			if name == "My Project" {
				if err != nil || len(streams) != 1 || streams[0].DatabaseName != name || streams[0].BackupCount != 42 {
					t.Fatalf("valid metadata changed: %#v, %v", streams, err)
				}
			} else if !errors.Is(err, serviceboundary.ErrReflectedCredential) || strings.Contains(err.Error(), numericServiceToken) {
				t.Fatalf("successful metadata reflection bypassed boundary: %v", err)
			}
		})
	}
}

func (decoderErrorBody) Close() error { return nil }

type decoderResponseTransport struct {
	body       io.ReadCloser
	seen       []*http.Request
	statusCode int
	header     http.Header
}

func (transport *decoderResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.seen = append(transport.seen, request)
	statusCode := transport.statusCode
	if statusCode == 0 {
		statusCode = http.StatusCreated
	}
	return &http.Response{StatusCode: statusCode, Header: transport.header.Clone(), Body: transport.body, Request: request}, nil
}

func TestServiceClientRedirectDiagnosticsDoNotReflectCredentials(t *testing.T) {
	for _, operation := range []string{"info", "upload", "download"} {
		t.Run(operation, func(t *testing.T) {
			client, err := NewServiceClient("http://127.0.0.1:8080", numericServiceToken)
			if err != nil {
				t.Fatal(err)
			}
			transport := &decoderResponseTransport{
				body: io.NopCloser(strings.NewReader("")), statusCode: http.StatusFound,
				header: http.Header{"Location": []string{"https://[" + numericServiceToken}},
			}
			client.client.Transport = transport
			switch operation {
			case "upload":
				snapshot := filepath.Join(t.TempDir(), "snapshot.aipdb")
				if err := os.WriteFile(snapshot, []byte("encrypted fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
				backup, replayed, uploadErr := client.Upload(t.Context(), "stream-a", "My Project", "install-a", "stable-operation", snapshot)
				err = uploadErr
				if backup != (ServiceBackup{}) || replayed {
					t.Fatal("redirect failure became a usable upload result or replay")
				}
			case "download":
				_, err = client.Download(t.Context(), "stream-a", "backup-a", filepath.Join(t.TempDir(), "download.aipdb"), 1024)
			default:
				_, err = client.Info(t.Context())
			}
			if !errors.Is(err, serviceboundary.ErrReflectedCredential) || strings.Contains(err.Error(), numericServiceToken) {
				t.Fatalf("client reflected a malformed Location token: %v", err)
			}
			if len(transport.seen) != 1 {
				t.Fatal("client dispatched a redirect")
			}
		})
	}
}

func TestServiceClientDownloadReaderDiagnosticCannotEscape(t *testing.T) {
	client, err := NewServiceClient("http://127.0.0.1:8080", numericServiceToken)
	if err != nil {
		t.Fatal(err)
	}
	client.client.Transport = &decoderResponseTransport{body: decoderErrorBody{}, statusCode: http.StatusOK}
	destination := filepath.Join(t.TempDir(), "download.aipdb")
	_, err = client.Download(t.Context(), "stream-a", "backup-a", destination, 1024)
	if !errors.Is(err, serviceboundary.ErrReflectedCredential) || strings.Contains(err.Error(), numericServiceToken) {
		t.Fatalf("download reader reflected the credential: %v", err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed download was retained: %v", err)
	}
}

func TestServiceClientBodyErrorsKeepSafeCancellationClasses(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errors.Join(context.Canceled, context.DeadlineExceeded)} {
		client, err := NewServiceClient("http://127.0.0.1:8080", numericServiceToken)
		if err != nil {
			t.Fatal(err)
		}
		client.client.Transport = &decoderResponseTransport{body: decoderErrorBody{cause: cause}}
		_, err = client.Info(t.Context())
		if err == nil || strings.Contains(err.Error(), numericServiceToken) {
			t.Fatalf("body cancellation diagnostic leaked: %v", err)
		}
		for _, standard := range []error{context.Canceled, context.DeadlineExceeded} {
			if errors.Is(err, standard) != errors.Is(cause, standard) {
				t.Fatalf("body cancellation class changed: %v", err)
			}
		}
	}
}

func TestServiceClientDecoderErrorsDoNotReflectCredentials(t *testing.T) {
	if err := ValidateServiceToken(numericServiceToken); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"upload", "streams", "info", "read failure"} {
		t.Run(operation, func(t *testing.T) {
			client, err := NewServiceClient("http://127.0.0.1:8080", numericServiceToken)
			if err != nil {
				t.Fatal(err)
			}
			payload := `{"size_bytes":` + numericServiceToken + `}`
			switch operation {
			case "streams":
				payload = `{"items":[{"backup_count":` + numericServiceToken + `}]}`
			case "info":
				payload = `{"max_upload_bytes":` + numericServiceToken + `}`
			}
			transport := &decoderResponseTransport{body: io.NopCloser(strings.NewReader(payload))}
			if operation == "read failure" {
				transport.body = decoderErrorBody{}
			}
			client.client.Transport = transport
			switch operation {
			case "upload":
				snapshot := filepath.Join(t.TempDir(), "snapshot.aipdb")
				if err := os.WriteFile(snapshot, []byte("encrypted fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
				backup, replayed, uploadErr := client.Upload(t.Context(), "stream-a", "My Project", "install-a", "stable-operation", snapshot)
				err = uploadErr
				if backup != (ServiceBackup{}) || replayed {
					t.Fatal("malformed upload metadata became a usable result or replay")
				}
			case "streams":
				_, err = client.ListStreams(t.Context())
			default:
				_, err = client.Info(t.Context())
			}
			if err == nil || strings.Contains(err.Error(), numericServiceToken) {
				t.Fatalf("service decoder returned credential-bearing diagnostics: %v", err)
			}
			var decodeError *json.UnmarshalTypeError
			if errors.As(err, &decodeError) {
				t.Fatal("unsafe typed decoder cause survived sanitization")
			}
			if len(transport.seen) != 1 || transport.seen[0].Header.Get("Authorization") != "Bearer "+numericServiceToken {
				t.Fatal("fixture did not exercise the authenticated service request")
			}
			if operation == "upload" && transport.seen[0].Header.Get("X-AIPermission-Operation-ID") != "stable-operation" {
				t.Fatal("upload identity changed on uncertain response")
			}
		})
	}
}
