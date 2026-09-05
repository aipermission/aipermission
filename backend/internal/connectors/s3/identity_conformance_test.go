package s3connector

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestS3ExactIdentityMinIO(t *testing.T) {
	endpoint := os.Getenv("AIPERMISSION_S3_IDENTITY_ENDPOINT")
	if endpoint == "" {
		t.Skip("requires dedicated disposable MinIO identity fixture")
	}
	runtime := s3TestRuntime(t, endpoint)
	runtime.Target.Config["bucket"] = "aipermission-conformance"
	runtime.Profile.Public["access_key_id"] = "conformance-access"
	runtime.Secrets = staticSecrets{"secret_access_key": "conformance-secret-key"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	client, err := newS3Client(ctx, runtime)
	if err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("identity-%d/", time.Now().UnixNano())
	for _, suffix := range []string{"invoice", "invoice ", " invoice", "a//b", "a/../b", "a/./b", "caf\u00e9", "cafe\u0301", "a%2Fb", "a+b?#", "a\\b", "/leading"} {
		t.Run(suffix, func(t *testing.T) {
			key := prefix + suffix
			if suffix == "/leading" {
				key = "/" + prefix + "leading"
			}
			sibling := path.Clean(key)
			if sibling == key {
				sibling = key + ".sibling"
			}
			if _, _, err := client.Do(ctx, http.MethodPut, sibling, nil, s3RequestBody{Data: []byte("untouched")}, maxS3ResponseBytes); err != nil {
				t.Fatal(err)
			}
			defer func() {
				_, _, _ = client.Do(context.Background(), http.MethodDelete, sibling, nil, nil, maxS3ResponseBytes)
			}()
			defer func() {
				data, _, err := client.Do(ctx, http.MethodGet, sibling, nil, nil, maxS3ResponseBytes)
				if err != nil || string(data) != "untouched" {
					t.Errorf("sibling changed: %q %v", data, err)
				}
			}()
			prepared, err := New().PrepareAction(ctx, connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: ActionUploadObject, Input: map[string]any{"key": key, "content_text": key, "overwrite": true}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = New().ExecuteAction(ctx, runtime, prepared)
			if err != nil {
				if expectedMinIOIdentityRejection(suffix, err) {
					t.Skipf("exact spelling unsupported by fixture (sibling remains unchanged): %v", err)
				}
				var status *s3StatusError
				if errors.As(err, &status) {
					t.Fatalf("unexpected provider rejection for %q: HTTP %d code %q: %v", suffix, status.status, status.code, err)
				}
				t.Fatal(err)
			}
			defer func() {
				_, _, _ = client.Do(context.Background(), http.MethodDelete, key, nil, nil, maxS3ResponseBytes)
			}()
			result, err := New().ExecuteAction(ctx, runtime, connectors.PreparedAction{ActionName: ActionDownloadObject, Payload: map[string]any{"key": key}})
			if err != nil {
				t.Fatal(err)
			}
			if got := result.Output.(map[string]any)["content_base64"]; got != base64.StdEncoding.EncodeToString([]byte(key)) {
				t.Fatalf("wrong object body: %v", got)
			}
			local := filepath.Join(t.TempDir(), "object")
			if _, err := DownloadFile(ctx, runtime, "/"+key, local, TransferOptions{}); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(local); err != nil || string(got) != key {
				t.Fatalf("transfer: %q %v", got, err)
			}
			signed, _, err := client.PresignObject(http.MethodGet, key, 60, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, signed, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.httpClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("signed GET status %d", response.StatusCode)
			}
		})
	}
}

func expectedMinIOIdentityRejection(suffix string, err error) bool {
	expected := map[string]string{
		"a//b":   "XMinioInvalidObjectName",
		"a/../b": "XMinioInvalidResourceName",
		"a/./b":  "XMinioInvalidResourceName",
	}[suffix]
	var status *s3StatusError
	return expected != "" && errors.As(err, &status) && status.status == http.StatusBadRequest && status.code == expected
}

func TestMinIOIdentityRejectionRequiresExactSuffixAndCode(t *testing.T) {
	for _, test := range []struct {
		suffix string
		status int
		code   string
		want   bool
	}{
		{"a//b", 400, "XMinioInvalidObjectName", true},
		{"a/../b", 400, "XMinioInvalidResourceName", true},
		{"a/./b", 400, "XMinioInvalidResourceName", true},
		{"invoice", 400, "XMinioInvalidObjectName", false},
		{"invoice ", 400, "XMinioInvalidObjectName", false},
		{"a//b", 400, "SignatureDoesNotMatch", false},
		{"a//b", 400, "MalformedXML", false},
		{"a//b", 400, "XMinioInvalidResourceName", false},
		{"a//b", 403, "XMinioInvalidObjectName", false},
		{"a//b", 400, "", false},
	} {
		err := &s3StatusError{status: test.status, code: test.code}
		if got := expectedMinIOIdentityRejection(test.suffix, err); got != test.want {
			t.Errorf("%q HTTP %d %q: got %v, want %v", test.suffix, test.status, test.code, got, test.want)
		}
	}
}
