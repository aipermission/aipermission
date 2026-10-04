package serviceboundary

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const fixtureToken = "backup-token-+/?&secret-0123456789"

func testBoundary(t *testing.T) *Boundary {
	t.Helper()
	boundary, err := New(" \t" + fixtureToken + "\n")
	if err != nil {
		t.Fatal(err)
	}
	return boundary
}

func reflectedForms() []string {
	forms := []string{}
	for _, value := range []string{fixtureToken, "Bearer " + fixtureToken} {
		forms = append(forms, value, url.QueryEscape(value), url.PathEscape(value),
			base64.StdEncoding.EncodeToString([]byte(value)), base64.RawStdEncoding.EncodeToString([]byte(value)),
			base64.URLEncoding.EncodeToString([]byte(value)), base64.RawURLEncoding.EncodeToString([]byte(value)))
	}
	return forms
}

func TestUnavailableBoundaryFailsClosed(t *testing.T) {
	for _, token := range []string{"", " \t\n"} {
		if boundary, err := New(token); boundary != nil || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("empty credential boundary = %v, %v", boundary, err)
		}
	}
	for _, boundary := range []*Boundary{nil, {}} {
		if err := boundary.CheckMetadata(map[string]string{"status": "safe"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("metadata without authority = %v", err)
		}
		if writer, err := boundary.NewScanningWriter(io.Discard); writer != nil || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("writer without authority = %v, %v", writer, err)
		}
	}
	if writer, err := testBoundary(t).NewScanningWriter(nil); writer != nil || err == nil {
		t.Fatalf("nil destination = %v, %v", writer, err)
	}
}

func TestMetadataRejectsCredentialFormsWithoutReflectingThem(t *testing.T) {
	boundary := testBoundary(t)
	for _, reflected := range reflectedForms() {
		err := boundary.CheckMetadata(map[string]any{"nested": []string{"prefix-" + reflected + "-suffix"}})
		if !errors.Is(err, ErrReflectedCredential) || strings.Contains(err.Error(), fixtureToken) {
			t.Fatalf("reflected metadata error = %v", err)
		}
	}
	if err := boundary.CheckMetadata(make(chan string)); err == nil || strings.Contains(err.Error(), fixtureToken) {
		t.Fatalf("invalid metadata error = %v", err)
	}
	if err := boundary.CheckMetadata(map[string]any{"name": "My Project", "size": 42}); err != nil {
		t.Fatalf("clean metadata = %v", err)
	}
}

func TestCleanStreamPreservesBytesAndFlushesExactlyOnce(t *testing.T) {
	payload := strings.Repeat("Encrypted fixture data.\n", 64)
	for _, chunk := range []int{1, 7, 37, len(payload)} {
		var destination bytes.Buffer
		writer, err := testBoundary(t).NewScanningWriter(&destination)
		if err != nil {
			t.Fatal(err)
		}
		if written, err := writer.Write(nil); written != 0 || err != nil {
			t.Fatalf("empty write = %d, %v", written, err)
		}
		for start := 0; start < len(payload); start += chunk {
			end := min(start+chunk, len(payload))
			if written, err := writer.Write([]byte(payload[start:end])); written != end-start || err != nil {
				t.Fatalf("clean write = %d, %v", written, err)
			}
		}
		if err := writer.Flush(); err != nil {
			t.Fatal(err)
		}
		if err := writer.Flush(); err != nil || destination.String() != payload {
			t.Fatalf("round-trip mismatch or repeated flush failure: %v", err)
		}
	}
}

func TestScanningWriterRejectsEveryCredentialSplit(t *testing.T) {
	boundary := testBoundary(t)
	for _, reflected := range reflectedForms() {
		for split := 1; split < len(reflected); split++ {
			var destination bytes.Buffer
			writer, err := boundary.NewScanningWriter(&destination)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write([]byte("safe-prefix-" + reflected[:split])); err != nil {
				if !errors.Is(err, ErrReflectedCredential) {
					t.Fatal(err)
				}
				continue
			}
			if _, err := writer.Write([]byte(reflected[split:] + "-suffix")); !errors.Is(err, ErrReflectedCredential) {
				t.Fatalf("split %d failed to reject reflected credential: %v", split, err)
			}
			if bytes.Contains(destination.Bytes(), []byte(reflected)) {
				t.Fatal("reflected credential reached the destination")
			}
		}
	}
}

func TestScanningWriterWithholdsCrossChunkCredentialPrefix(t *testing.T) {
	var destination bytes.Buffer
	writer, err := testBoundary(t).NewScanningWriter(&destination)
	if err != nil {
		t.Fatal(err)
	}
	split := len(fixtureToken) / 2
	first := []byte(strings.Repeat(":", 4096) + fixtureToken[:split])
	if written, err := writer.Write(first); err != nil || written != len(first) {
		t.Fatalf("write credential prefix = %d, %v", written, err)
	}
	if destination.Len() == 0 || strings.Contains(destination.String(), fixtureToken[:split]) {
		t.Fatal("did not stream safe bytes while withholding the possible credential prefix")
	}
	if _, err := writer.Write([]byte(fixtureToken[split:])); !errors.Is(err, ErrReflectedCredential) {
		t.Fatalf("cross-chunk credential = %v", err)
	}
	if strings.Contains(destination.String(), fixtureToken) {
		t.Fatal("reflected credential reached destination")
	}
}

func TestSharedBoundaryOwnsIndependentConcurrentWriters(t *testing.T) {
	boundary := testBoundary(t)
	var group sync.WaitGroup
	for range 12 {
		group.Go(func() {
			var destination bytes.Buffer
			writer, err := boundary.NewScanningWriter(&destination)
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := writer.Write([]byte("clean")); err != nil {
				t.Error(err)
			}
			if err := writer.Flush(); err != nil || destination.String() != "clean" {
				t.Errorf("writer state was not isolated: %v", err)
			}
			if err := boundary.CheckMetadata(map[string]string{"status": "clean"}); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
}
