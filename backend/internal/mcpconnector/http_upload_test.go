package mcpconnector

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func uploadMetadataFixture() actionUploadMetadata {
	return actionUploadMetadata{
		TargetRef: "ssh:1:1", ActionName: "upload_file", Reason: "Send fixture", IdempotencyKey: "upload-fixture",
		Manifest: connectors.FileUploadManifest{FileName: "fixture.bin", RemotePath: "/tmp/fixture.bin", SizeBytes: 3, ChecksumSHA256: strings.Repeat("a", 64)},
	}
}

func uploadMultipartRequest(t *testing.T, metadata string, names ...string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("metadata", metadata); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		part, err := writer.CreateFormFile(name, "fixture.bin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte{0, 255, 42}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func uploadScopeFixture() ActionScope {
	database := &sql.DB{}
	return ActionScope{
		Database: database, RuntimeID: "upload-runtime", TokenID: 1,
		Output: &OutputAuthorization{
			Database: database, Tokens: tokens.NewStore(database), Leases: vaultsessions.NewStore(),
			Delivery: acceptingDeliveryGate{}, MCPStarted: func() bool { return true },
			Principal: func(int64) (executionprincipal.Principal, error) { return executionprincipal.Principal{}, nil },
		},
		ActionVisible: func(context.Context, string, string) (bool, error) { return true, nil },
		ReplayExists:  func(context.Context, string) (bool, error) { return false, nil },
		ResourcePolicy: func(context.Context, string, string) (ActionResourcePolicy, error) {
			return ActionResourcePolicy{MaxInputBytes: 1024, MaxUploadBytes: 64}, nil
		},
		Call: func(context.Context, ActionCall) (ActionCallResult, error) {
			return ActionCallResult{}, errors.New("fixture dispatch refused")
		},
		Observe: func(context.Context, string, any) {},
		Redact:  func(_ context.Context, value string) string { return value },
		StageUpload: func(context.Context, connectors.FileUploadStageRequest, io.Reader) error {
			panic("rejected upload reached staging")
		},
		AcquireUploadDispatch: func(context.Context) (func(), error) { return func() {}, nil },
		ValidateUpload:        func(context.Context, string, string, map[string]any, string) error { return nil },
	}
}

func TestUploadHTTPRejectsBeforeStaging(t *testing.T) {
	for _, name := range []string{"missing-composition", "stopped", "target", "action", "key", "reason-bytes", "no-reason", "invalid-manifest", "replay-read", "invisible", "policy-read", "unsupported", "too-large", "input-budget", "validation", "upload-slots", "missing-file", "wrong-part"} {
		t.Run(name, func(t *testing.T) {
			scope, metadata := uploadScopeFixture(), uploadMetadataFixture()
			h := NewActionHTTPHandlers(func(http.ResponseWriter, *http.Request) (ActionScope, bool) { return scope, true })
			want := http.StatusBadRequest
			parts := []string{"file"}
			switch name {
			case "missing-composition":
				scope.StageUpload, want = nil, http.StatusInternalServerError
			case "stopped":
				scope.Output.MCPStarted, want = func() bool { return false }, http.StatusOK
			case "no-reason":
				metadata.Reason = " \t"
			case "target":
				metadata.TargetRef = " "
			case "action":
				metadata.ActionName = "invalid action"
			case "key":
				metadata.IdempotencyKey = strings.Repeat("r", 129)
			case "reason-bytes":
				metadata.Reason = strings.Repeat("\u03bb", maxReasonBytes/2+1)
			case "invalid-manifest":
				metadata.Manifest.SizeBytes = -1
			case "replay-read":
				scope.ReplayExists = func(context.Context, string) (bool, error) { return false, errors.New("private read failure") }
				want = http.StatusInternalServerError
			case "invisible":
				scope.ActionVisible = func(context.Context, string, string) (bool, error) { return false, nil }
				want = http.StatusNotFound
			case "policy-read":
				scope.ResourcePolicy = func(context.Context, string, string) (ActionResourcePolicy, error) {
					return ActionResourcePolicy{}, errors.New("private policy failure")
				}
				want = http.StatusInternalServerError
			case "unsupported":
				scope.ResourcePolicy = func(context.Context, string, string) (ActionResourcePolicy, error) {
					return ActionResourcePolicy{MaxInputBytes: 1024}, nil
				}
			case "too-large":
				metadata.Manifest.SizeBytes, want = 65, http.StatusRequestEntityTooLarge
			case "input-budget":
				scope.ResourcePolicy = func(context.Context, string, string) (ActionResourcePolicy, error) {
					return ActionResourcePolicy{MaxInputBytes: 1, MaxUploadBytes: 64}, nil
				}
				want = http.StatusRequestEntityTooLarge
			case "validation":
				scope.ValidateUpload = func(context.Context, string, string, map[string]any, string) error {
					return errors.New("fixture validation refused")
				}
			case "upload-slots":
				h.uploads <- struct{}{}
				h.uploads <- struct{}{}
				want = http.StatusTooManyRequests
			case "wrong-part":
				parts = []string{"unexpected"}
			case "missing-file":
				parts = nil
			}
			encoded, err := json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			h.Upload(response, uploadMultipartRequest(t, string(encoded), parts...))
			if response.Code != want || strings.Contains(response.Body.String(), "private") {
				t.Fatalf("upload guard=%d %s want=%d", response.Code, response.Body.String(), want)
			}
		})
	}
}

func TestUploadMetadataRejectsWrongEnvelopeAndRetainsExactIdentity(t *testing.T) {
	metadata := uploadMetadataFixture()
	encoded, _ := json.Marshal(metadata)
	request := uploadMultipartRequest(t, string(encoded), "file")
	reader, decoded, err := readUploadMetadata(request)
	if err != nil || decoded != metadata {
		t.Fatalf("manifest changed: %#v error=%v", decoded, err)
	}
	part, err := reader.NextRawPart()
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(&finalUploadPart{part: part, reader: reader})
	if err != nil || !bytes.Equal(content, []byte{0, 255, 42}) {
		t.Fatalf("binary content changed: %v %v", content, err)
	}
	for _, content := range []string{"{", `{"unknown":1}`, strings.Repeat(" ", maxUploadMetadataBytes+1)} {
		if _, _, err := readUploadMetadata(uploadMultipartRequest(t, content)); !errors.Is(err, errUploadMultipart) {
			t.Fatalf("invalid metadata accepted: %v", err)
		}
	}
	for _, header := range []string{"text/plain", "multipart/form-data", "multipart/form-data; boundary=" + strings.Repeat("x", 71)} {
		request := uploadMultipartRequest(t, string(encoded))
		request.Header.Set("Content-Type", header)
		if _, _, err := readUploadMetadata(request); !errors.Is(err, errUploadMultipart) {
			t.Fatalf("invalid media type accepted: %v", err)
		}
	}
	request = uploadMultipartRequest(t, string(encoded), "file", "extra")
	reader, _, _ = readUploadMetadata(request)
	part, _ = reader.NextRawPart()
	if _, err := io.ReadAll(&finalUploadPart{part: part, reader: reader}); !errors.Is(err, errUploadMultipart) {
		t.Fatalf("additional upload part accepted: %v", err)
	}
}
