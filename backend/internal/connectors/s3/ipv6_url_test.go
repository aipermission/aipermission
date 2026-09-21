package s3connector

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestS3ClientRejectsVirtualHostAddressingForIPLiteralEndpoints(t *testing.T) {
	tests := []string{
		"http://192.0.2.10:9000",
		"http://[2001:db8::1]:9000",
	}
	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			runtime := s3TestRuntime(t, rawURL)
			runtime.Target.Config["path_style"] = false
			_, err := newS3Client(context.Background(), runtime)
			if err == nil || !strings.Contains(err.Error(), "enable path_style for IP endpoints") {
				t.Fatalf("newS3Client() error = %v", err)
			}
		})
	}
}

func TestS3URLPreservesHostAtDefaultAndExplicitPorts(t *testing.T) {
	tests := []struct {
		name   string
		scheme string
		host   string
		port   int
	}{
		{name: "dns default", scheme: "https", host: "objects.example", port: 443},
		{name: "ipv4 default", scheme: "http", host: "192.0.2.10", port: 80},
		{name: "ipv6 http default", scheme: "http", host: "2001:db8::1", port: 80},
		{name: "ipv6 https default", scheme: "https", host: "2001:db8::1", port: 443},
		{name: "ipv6 explicit", scheme: "http", host: "2001:db8::1", port: 9000},
		{name: "ipv6 zone default", scheme: "https", host: "fe80::1%eth0", port: 443},
		{name: "bracketed ipv6 default", scheme: "https", host: "[2001:db8::2]", port: 443},
		{name: "bracketed ipv6 explicit", scheme: "http", host: "[2001:db8::2]", port: 9000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			targetView := connectors.TargetView{Config: map[string]any{"host": test.host}}
			host := s3Host(targetView)
			client := &s3Client{scheme: test.scheme, host: host, port: test.port, bucket: "fixture", pathStyle: true}
			target := client.URL("object.bin", nil)
			wantHost := strings.Trim(test.host, "[]")
			if target.Hostname() != wantHost {
				t.Fatalf("URL %q hostname = %q, want %q", target.String(), target.Hostname(), wantHost)
			}
			if _, err := url.ParseRequestURI(target.String()); err != nil {
				t.Fatalf("URL %q is invalid: %v", target.String(), err)
			}
		})
	}
}

func TestPresignedS3URLPreservesDefaultPortIPv6Host(t *testing.T) {
	client := &s3Client{
		scheme: "https", host: "2001:db8::1", port: 443, region: "us-east-1",
		bucket: "fixture", pathStyle: true, accessKey: "access", secretKey: "secret",
	}
	raw, _, err := client.PresignObject(http.MethodGet, "object.bin", minPresignedExpirySeconds, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Hostname() != client.host {
		t.Fatalf("presigned URL %q hostname = %q, want %q", raw, parsed.Hostname(), client.host)
	}
}
