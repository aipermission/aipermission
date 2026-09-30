package backup

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestImportBodyBoundsSilentHTTPAndPartialMultipartHeaders(t *testing.T) {
	for _, partial := range []string{"", "--example\r\nContent-Dis"} {
		t.Run(partial, func(t *testing.T) {
			observed := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				guarded, finish := guardImportBody(w, r, 25*time.Millisecond)
				defer finish()
				err := guarded.ParseMultipartForm(8 << 20)
				if guarded.MultipartForm != nil {
					defer guarded.MultipartForm.RemoveAll()
				}
				if err != nil && guarded.Context().Err() != nil {
					observed <- context.Cause(guarded.Context())
					w.WriteHeader(http.StatusRequestTimeout)
					return
				}
				observed <- err
				w.WriteHeader(http.StatusBadRequest)
			}))
			defer server.Close()
			connection, err := net.DialTimeout("tcp", strings.TrimPrefix(server.URL, "http://"), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			_, err = io.WriteString(connection, "POST /api/backup/import HTTP/1.1\r\nHost: localhost\r\nContent-Type: multipart/form-data; boundary=example\r\nContent-Length: 1000\r\n\r\n"+partial)
			if err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(bufio.NewReader(connection), nil)
			if err != nil {
				t.Fatalf("silent connection did not return a bounded response: %v", err)
			}
			defer response.Body.Close()
			bodyError := <-observed
			if response.StatusCode != http.StatusRequestTimeout {
				t.Fatalf("status=%d body error=%v", response.StatusCode, bodyError)
			}
			if !errors.Is(bodyError, errImportBodyIdle) && !errors.Is(bodyError, context.Canceled) {
				t.Fatalf("body error=%v", bodyError)
			}
		})
	}
}
