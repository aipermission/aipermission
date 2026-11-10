package mcpconnector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestUploadDispatchRequiresAndReleasesWorkspaceAdmission(t *testing.T) {
	for _, mode := range []string{"unavailable", "missing-release", "admitted"} {
		t.Run(mode, func(t *testing.T) {
			scope := uploadScopeFixture()
			calls, releases := 0, 0
			scope.AcquireUploadDispatch = func(context.Context) (func(), error) {
				if mode == "unavailable" {
					return nil, errors.New("private workspace failure")
				}
				if mode == "missing-release" {
					return nil, nil
				}
				return func() { releases++ }, nil
			}
			scope.Call = func(_ context.Context, call ActionCall) (ActionCallResult, error) {
				calls++
				if call.Source != "mcp" || call.TokenID != scope.TokenID || call.IdempotencyKey != "exact-upload" {
					t.Fatalf("dispatch identity changed: %#v", call)
				}
				return ActionCallResult{}, errors.New("fixture dispatch refused")
			}
			response := httptest.NewRecorder()
			dispatchUpload(response, httptest.NewRequest(http.MethodPost, "/upload", nil), scope,
				ActionCallRequest{TargetRef: "ssh:1:1", ActionName: "upload_file", IdempotencyKey: "exact-upload"}, 1024)
			want, count := http.StatusServiceUnavailable, 0
			if mode == "admitted" {
				want, count = http.StatusBadRequest, 1
			}
			if response.Code != want || calls != count || releases != count || strings.Contains(response.Body.String(), "private") {
				t.Fatalf("dispatch fence=%d %s calls=%d releases=%d", response.Code, response.Body.String(), calls, releases)
			}
		})
	}
}

func TestUploadErrorsRetainPublicCodesWithoutInternalDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"body-limit", &http.MaxBytesError{Limit: 1}, http.StatusRequestEntityTooLarge, "upload_too_large"},
		{"capacity", connectors.ErrUploadStagingCapacity, http.StatusTooManyRequests, "upload_staging_capacity"},
		{"conflict", connectors.ErrUploadStageConflict, http.StatusConflict, ""},
		{"expired", connectors.ErrUploadStageExpired, http.StatusConflict, "upload_stage_expired"},
		{"parts", errUploadMultipart, http.StatusBadRequest, ""},
		{"content", connectors.ErrUploadContentChanged, http.StatusBadRequest, ""},
		{"manifest", connectors.ErrInvalidUploadManifest, http.StatusBadRequest, ""},
		{"target", connectortargets.ErrTargetProfileNotFound, http.StatusNotFound, ""},
		{"internal", errors.New("private storage failure"), http.StatusInternalServerError, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeUploadError(response, httptest.NewRequest(http.MethodPost, "/upload", nil), uploadScopeFixture(), test.err)
			if response.Code != test.status || strings.Contains(response.Body.String(), "private") || (test.code != "" && !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`)) {
				t.Fatalf("upload error=%d %s", response.Code, response.Body.String())
			}
		})
	}
}
