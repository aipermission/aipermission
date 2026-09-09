package sigv4

import (
	"net/http"
	"testing"
)

func TestCanonicalRequestUsesURLHostAndConditionalHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodPut, "https://s3.example.com/bucket/report.csv", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Amz-Date", "20260909T120000Z")
	req.Header.Set("X-Amz-Content-Sha256", EmptyPayloadHash)
	req.Header.Set("If-Match", `"etag"`)
	req.Header.Set("If-None-Match", "*")

	canonical, signedHeaders := CanonicalRequest(req, EmptyPayloadHash)
	if signedHeaders != "host;if-match;if-none-match;x-amz-content-sha256;x-amz-date" {
		t.Fatalf("signed headers = %q", signedHeaders)
	}
	wantHash := "3bbd1b3dc222cd7d79016a5b52b953ef2135f409aafc340bba3f38e5dd37299f"
	if got := SHA256Hex([]byte(canonical)); got != wantHash {
		t.Fatalf("canonical request hash = %q, want %q\n%s", got, wantHash, canonical)
	}
}

func TestEscapingPreservesS3ObjectPathIdentity(t *testing.T) {
	if got, want := PathEscape("folder name/a+b?#"), "folder%20name/a%2Bb%3F%23"; got != want {
		t.Fatalf("path escape = %q, want %q", got, want)
	}
	if got, want := QueryEscape(" version +/? "), "%20version%20%2B%2F%3F%20"; got != want {
		t.Fatalf("query escape = %q, want %q", got, want)
	}
}
