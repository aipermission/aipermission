package s3connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestExecuteListObjectVersionsFiltersExactKeyAndReturnsCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["versions"]; !ok {
			t.Fatalf("missing versions query: %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("prefix") != "daily/report.csv" {
			t.Fatalf("prefix = %q", r.URL.Query().Get("prefix"))
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<ListVersionsResult>
<IsTruncated>true</IsTruncated>
<NextKeyMarker>daily/report.csv</NextKeyMarker>
<NextVersionIdMarker>version-1</NextVersionIdMarker>
<Version><Key>daily/report.csv</Key><VersionId>version-1</VersionId><IsLatest>true</IsLatest><LastModified>2026-08-11T10:00:00Z</LastModified><ETag>"abc"</ETag><Size>42</Size><StorageClass>STANDARD</StorageClass></Version>
<Version><Key>daily/report.csv.extra</Key><VersionId>other</VersionId><IsLatest>false</IsLatest><LastModified>2026-08-10T10:00:00Z</LastModified><ETag>"def"</ETag><Size>12</Size></Version>
<DeleteMarker><Key>daily/report.csv</Key><VersionId>marker-1</VersionId><IsLatest>false</IsLatest><LastModified>2026-08-09T10:00:00Z</LastModified></DeleteMarker>
</ListVersionsResult>`))
	}))
	defer server.Close()

	result, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionListVersions,
		Payload:    map[string]any{"key": "daily/report.csv", "cursor": "", "limit": 100},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	output := result.Output.(map[string]any)
	if output["count"] != 2 || output["next_cursor"] == "" {
		t.Fatalf("output = %#v", output)
	}
	versions := output["versions"].([]map[string]any)
	if versions[0]["version_id"] != "version-1" || versions[1]["delete_marker"] != true {
		t.Fatalf("versions = %#v", versions)
	}
	cursor, err := decodeVersionCursor(output["next_cursor"].(string))
	if err != nil || cursor.VersionIDMarker != "version-1" {
		t.Fatalf("cursor = %#v err=%v", cursor, err)
	}
}

func TestExecuteListObjectVersionsSkipsEmptyPrefixCollisionPages(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("max-keys") != "100" {
			t.Fatalf("default max-keys = %q", r.URL.Query().Get("max-keys"))
		}
		if calls == 1 {
			_, _ = w.Write([]byte(`<ListVersionsResult>
<IsTruncated>true</IsTruncated><NextKeyMarker>a-extra</NextKeyMarker><NextVersionIdMarker>other</NextVersionIdMarker>
<Version><Key>a-extra</Key><VersionId>other</VersionId></Version>
</ListVersionsResult>`))
			return
		}
		if r.URL.Query().Get("key-marker") != "a-extra" || r.URL.Query().Get("version-id-marker") != "other" {
			t.Fatalf("marker query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`<ListVersionsResult>
<Version><Key>a</Key><VersionId>exact</VersionId><LastModified>2026-08-11T10:00:00Z</LastModified></Version>
</ListVersionsResult>`))
	}))
	defer server.Close()

	result, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionListVersions,
		Payload:    map[string]any{"key": "a"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	output := result.Output.(map[string]any)
	if calls != 2 || output["count"] != 1 {
		t.Fatalf("calls=%d output=%#v", calls, output)
	}
}

func TestRestoreObjectVersionUsesVersionedCopySource(t *testing.T) {
	var copySource string
	var expectedETag string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		copySource = r.Header.Get("X-Amz-Copy-Source")
		expectedETag = r.Header.Get("If-Match")
		_, _ = w.Write([]byte(`<CopyObjectResult><ETag>&quot;restored-etag&quot;</ETag></CopyObjectResult>`))
	}))
	defer server.Close()

	_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionRestoreVersion,
		Payload:    map[string]any{"key": "daily/report #1.csv", "version_id": "v/1+two", "expected_current_etag": "etag-current"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if copySource != "/test-bucket/daily/report%20%231.csv?versionId=v%2F1%2Btwo" {
		t.Fatalf("copy source = %q", copySource)
	}
	if expectedETag != `"etag-current"` {
		t.Fatalf("destination if-match = %q", expectedETag)
	}
}

func TestRestoreObjectVersionCanGuardAbsentCurrentObject(t *testing.T) {
	var ifNoneMatch string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ifNoneMatch = r.Header.Get("If-None-Match")
		_, _ = w.Write([]byte(`<CopyObjectResult><ETag>&quot;restored-etag&quot;</ETag></CopyObjectResult>`))
	}))
	defer server.Close()

	_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionRestoreVersion,
		Payload:    map[string]any{"key": "daily/report.csv", "version_id": "version-1", "expected_current_absent": true},
	})
	if err != nil {
		t.Fatalf("execute absent-current restore: %v", err)
	}
	if ifNoneMatch != "*" {
		t.Fatalf("destination if-none-match = %q", ifNoneMatch)
	}
}

func TestRestoreObjectVersionRequiresExactlyOneDestinationGuard(t *testing.T) {
	for _, input := range []map[string]any{
		{"key": "daily/report.csv", "version_id": "version-1"},
		{"key": "daily/report.csv", "version_id": "version-1", "expected_current_etag": "etag", "expected_current_absent": true},
	} {
		_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
			Target: s3TestTarget(t, "http://127.0.0.1:9000"), Profile: s3TestProfile(), ActionName: ActionRestoreVersion, Input: input,
		})
		if err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("input=%#v error=%v", input, err)
		}
	}
}

func TestRestoreObjectVersionRejectsEmbeddedCopyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<Error><Code>InternalError</Code><Message>copy failed after acceptance</Message></Error>`))
	}))
	defer server.Close()

	_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionRestoreVersion,
		Payload:    map[string]any{"key": "daily/report.csv", "version_id": "version-1", "expected_current_etag": "etag-current"},
	})
	if err == nil || !strings.Contains(err.Error(), "copy failed after acceptance") {
		t.Fatalf("expected embedded copy error, got %v", err)
	}
}

func TestRestoreObjectVersionClassifiesUnconfirmedSuccessAsOutcomeUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<not-a-copy-result>`))
	}))
	defer server.Close()

	_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionRestoreVersion,
		Payload:    map[string]any{"key": "daily/report.csv", "version_id": "version-1", "expected_current_etag": "etag-current"},
	})
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
		t.Fatalf("error = %v, status = %q", err, connectors.ErrorStatus(err))
	}
}

func TestRestoreObjectVersionClassifiesConditionalConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`<Error><Code>ConditionalRequestConflict</Code><Message>destination changed</Message></Error>`))
	}))
	defer server.Close()

	_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionRestoreVersion,
		Payload:    map[string]any{"key": "daily/report.csv", "version_id": "version-1", "expected_current_etag": "etag-current"},
	})
	if connectors.ErrorCode(err) != "precondition_failed" {
		t.Fatalf("error = %v, code = %q", err, connectors.ErrorCode(err))
	}
}

func TestDeleteObjectVersionUsesExactVersionID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Query().Get("versionId") != "version+1/2" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("If-Match") != "" {
			t.Fatalf("version delete must not guard the current version ETag: %q", r.Header.Get("If-Match"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	_, err := New().ExecuteAction(context.Background(), s3TestRuntime(t, server.URL), connectors.PreparedAction{
		ActionName: ActionDeleteVersion,
		Payload:    map[string]any{"key": "daily/report.csv", "version_id": "version+1/2"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestPrepareDeleteObjectVersionDropsObsoleteETagAndIsIdempotent(t *testing.T) {
	prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target: s3TestTarget(t, "http://127.0.0.1:9000"), Profile: s3TestProfile(), ActionName: ActionDeleteVersion,
		Input: map[string]any{"key": "daily/report.csv", "version_id": "version-1", "expected_etag": "stale-current-etag"},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, exists := prepared.Payload["expected_etag"]; exists {
		t.Fatalf("prepared payload retained obsolete ETag: %#v", prepared.Payload)
	}
	if prepared.RetryPolicy == nil || prepared.RetryPolicy.Class != connectors.RetryIdempotent {
		t.Fatalf("retry policy = %#v", prepared.RetryPolicy)
	}
}

func TestPrepareDeleteObjectVersionRejectsMutableNullVersion(t *testing.T) {
	_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target: s3TestTarget(t, "http://127.0.0.1:9000"), Profile: s3TestProfile(), ActionName: ActionDeleteVersion,
		Input: map[string]any{"key": "daily/report.csv", "version_id": "null"},
	})
	if err == nil || !strings.Contains(err.Error(), "not accepted") {
		t.Fatalf("expected mutable null version rejection, got %v", err)
	}
}

func TestPrepareObjectVersionActionsUseExplicitRisks(t *testing.T) {
	connector := New()
	for _, test := range []struct {
		name   string
		action string
		risk   connectors.RiskLevel
	}{
		{name: "list", action: ActionListVersions, risk: connectors.RiskRead},
		{name: "restore", action: ActionRestoreVersion, risk: connectors.RiskWrite},
		{name: "delete", action: ActionDeleteVersion, risk: connectors.RiskDestructive},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := map[string]any{"key": "/daily/report.csv"}
			if test.action != ActionListVersions {
				input["version_id"] = "version-1"
			}
			if test.action == ActionRestoreVersion {
				input["expected_current_etag"] = "etag-current"
			}
			prepared, err := connector.PrepareAction(context.Background(), connectors.ActionRequest{
				Target: s3VerifiedTestTarget(t, "http://127.0.0.1:9000"), Profile: s3TestProfile(), ActionName: test.action, Input: input,
			})
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if prepared.Risk != test.risk || prepared.Payload["key"] != "daily/report.csv" {
				t.Fatalf("prepared = %#v", prepared)
			}
		})
	}
}

func TestDecodeVersionCursorRejectsInvalidInput(t *testing.T) {
	if _, err := decodeVersionCursor("not-a-valid-cursor"); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("err = %v", err)
	}
}
