package s3connector

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestS3ClientPreservesStoredContentEncodingBytes(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := io.WriteString(writer, "stored object bytes"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept-Encoding"); got != "identity" {
			t.Fatalf("Accept-Encoding = %q", got)
		}
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(compressed.Bytes())
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &s3Client{
		scheme: endpoint.Scheme, host: endpoint.Hostname(), port: mustTestPort(t, endpoint),
		bucket: "fixture", pathStyle: true, region: "us-east-1",
		accessKey: "access", secretKey: "secret", httpClient: server.Client(),
	}
	data, headers, err := client.GetObject(context.Background(), "compressed.bin", compressed.Len()+1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, compressed.Bytes()) || headers.Get("Content-Encoding") != "gzip" {
		t.Fatalf("object bytes changed: got=%x want=%x headers=%v", data, compressed.Bytes(), headers)
	}
}

func mustTestPort(t *testing.T, endpoint *url.URL) int {
	t.Helper()
	port := endpoint.Port()
	if port == "" {
		t.Fatal("test endpoint omitted port")
	}
	parsed, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
