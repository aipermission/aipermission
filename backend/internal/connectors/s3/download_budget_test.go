package s3connector

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type downloadPipeTransport struct {
	t        *testing.T
	response string
	tail     string
	progress <-chan struct{}
}

func (downloadPipeTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (transport downloadPipeTransport) DialConnectorTCP(ctx context.Context, _ connectors.NetworkDialRequest) (net.Conn, error) {
	client, server := net.Pipe()
	done := make(chan struct{})
	transport.t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
		<-done
	})
	go func() {
		defer close(done)
		defer server.Close()
		request, err := http.ReadRequest(bufio.NewReader(server))
		if err != nil {
			return
		}
		_ = request.Body.Close()
		if request.Method != http.MethodGet || request.Header.Get("Accept-Encoding") != "identity" {
			transport.t.Errorf("unexpected download request: %s %#v", request.Method, request.Header)
			return
		}
		if _, err := io.WriteString(server, transport.response); err != nil || transport.tail == "" {
			return
		}
		select {
		case <-transport.progress:
		case <-ctx.Done():
			return
		}
		_, _ = io.WriteString(server, transport.tail)
	}()
	return client, nil
}

type downloadPipeCapabilities struct{ transport downloadPipeTransport }

func (capabilities downloadPipeCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.NetworkTransportCapabilityName {
		return capabilities.transport
	}
	return nil
}

func TestDownloadBudgetS3PreservesWrittenBytesAndRetainsPartialStaging(t *testing.T) {
	for _, test := range []struct {
		name, response, tail   string
		bytes                  int64
		success, limit, cancel bool
	}{
		{name: "exact limit", response: "HTTP/1.1 200 OK\r\nContent-Length: 8\r\n\r\n12345678", bytes: 8, success: true},
		{name: "oversized header", response: "HTTP/1.1 200 OK\r\nContent-Length: 9\r\n\r\n123456789", limit: true},
		{name: "growing stream", response: "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4\r\n1234\r\n", tail: "5\r\n56789\r\n0\r\n\r\n", bytes: 4, limit: true},
		{name: "partial failure", response: "HTTP/1.1 200 OK\r\nContent-Length: 8\r\n\r\n1234", bytes: 4},
		{name: "canceled after write", response: "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4\r\n1234\r\n", tail: "0\r\n\r\n", bytes: 4, cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			progress := make(chan struct{})
			var once sync.Once
			runtime := s3TestRuntime(t, "http://s3.invalid:80")
			runtime.Capabilities = downloadPipeCapabilities{transport: downloadPipeTransport{t: t, response: test.response, tail: test.tail, progress: progress}}
			path := filepath.Join(t.TempDir(), "download")
			result, err := DownloadFile(ctx, runtime, "/object", path, TransferOptions{
				MaxBytes: 8,
				Progress: func(int64, int64) {
					if test.cancel {
						cancel()
					}
					once.Do(func() { close(progress) })
				},
			})
			if (err == nil) != test.success || result.Bytes != test.bytes || errors.Is(err, connectors.ErrTransferByteLimit) != test.limit {
				t.Fatalf("lost limit or written-byte evidence: result=%#v err=%v", result, err)
			}
			if test.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if test.success {
				info, err := os.Stat(path)
				if err != nil || info.Size() != test.bytes || result.ChecksumSHA256 == "" {
					t.Fatalf("exact-limit file was not published: info=%v err=%v result=%#v", info, err, result)
				}
			} else {
				info, statErr := os.Lstat(path)
				if result.ChecksumSHA256 != "" || (test.bytes == 0 && !os.IsNotExist(statErr)) || (test.bytes > 0 && (statErr != nil || info.Size() != test.bytes)) {
					t.Fatalf("partial evidence lost or published as complete: stat=%v result=%#v", statErr, result)
				}
			}
		})
	}
}
