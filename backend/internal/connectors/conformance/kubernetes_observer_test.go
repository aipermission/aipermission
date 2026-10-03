package conformance_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type kubeObserver struct {
	client *http.Client
	token  string
}

func newKubeObserver(t *testing.T) kubeObserver {
	t.Helper()
	if os.Getenv("AIPERMISSION_KUBERNETES_HOST") != "kube-api" {
		t.Fatal("Kubernetes API conformance requires the owned agentless fixture")
	}
	certificate, err := os.ReadFile("/kube-material/ca.crt")
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		t.Fatal("owned Kubernetes CA is invalid")
	}
	token, err := os.ReadFile("/kube-material/observer-token")
	if err != nil || len(bytes.TrimSpace(token)) == 0 {
		t.Fatal("owned Kubernetes observer token is missing")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	return kubeObserver{client: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, token: strings.TrimSpace(string(token))}
}

func (observer kubeObserver) request(t *testing.T, ctx context.Context, method, path string, body map[string]any, status int) map[string]any {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, method, "https://kube-api:6443"+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+observer.token)
	request.Header.Set("Content-Type", "application/json")
	if method == http.MethodPatch {
		request.Header.Set("Content-Type", "application/merge-patch+json")
	}
	response, err := observer.client.Do(request)
	if err != nil {
		t.Fatalf("owned Kubernetes observer request: %v", err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		message, _ := result["message"].(string)
		message = strings.ReplaceAll(message, observer.token, "[REDACTED]")
		if len(message) > 1000 {
			message = message[:1000]
		}
		t.Fatalf("Kubernetes observer %s %s: status=%d, want=%d, reason=%v, message=%s", method, path, response.StatusCode, status, result["reason"], message)
	}
	return result
}

func (observer kubeObserver) scoped(t *testing.T) kubeObserver {
	t.Helper()
	token, err := os.ReadFile("/kube-material/scoped-token")
	if err != nil || len(bytes.TrimSpace(token)) == 0 {
		t.Fatal("owned Kubernetes scoped token is missing")
	}
	observer.token = strings.TrimSpace(string(token))
	return observer
}

func resourceMetadata(t *testing.T, resource map[string]any) map[string]any {
	t.Helper()
	metadata, ok := resource["metadata"].(map[string]any)
	if !ok || metadata["resourceVersion"] == "" || metadata["resourceVersion"] == nil {
		t.Fatal("real Kubernetes resource has no server-assigned version")
	}
	return metadata
}
