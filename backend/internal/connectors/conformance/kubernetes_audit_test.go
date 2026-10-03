package conformance_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"
)

type kubeAuditEvent struct {
	Level   string `json:"level"`
	Stage   string `json:"stage"`
	Verb    string `json:"verb"`
	AuditID string `json:"auditID"`
	User    struct {
		Username string `json:"username"`
	} `json:"user"`
	ObjectRef      struct{ Resource, Namespace, Name string } `json:"objectRef"`
	ResponseStatus struct{ Code int }                         `json:"responseStatus"`
	RequestObject  json.RawMessage                            `json:"requestObject"`
	ResponseObject json.RawMessage                            `json:"responseObject"`
}

func assertKubeServerPatchWitnesses(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		counts := readKubePatchWitnesses(t)
		if counts[200] == 1 && counts[409] == 1 && counts[403] == 1 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Kubernetes audit witness canceled: %v", ctx.Err())
		case <-deadline.C:
			t.Fatalf("real API did not witness accepted/conflicting/denied patches exactly once: %#v", counts)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func readKubePatchWitnesses(t *testing.T) map[int]int {
	t.Helper()
	file, err := os.Open("/kube-material/audit.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > 1<<20 {
		t.Fatal("fixture audit exceeded its bounded readback window")
	}
	scanner := bufio.NewScanner(io.LimitReader(file, 1<<20))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	// The server may still be appending the last record. Only complete JSONL
	// frames are eligible witnesses; malformed complete frames remain errors.
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if atEOF && bytes.IndexByte(data, '\n') < 0 {
			return len(data), nil, nil
		}
		return bufio.ScanLines(data, atEOF)
	})
	counts := map[int]int{}
	seen := map[string]bool{}
	for scanner.Scan() {
		var event kubeAuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if len(event.RequestObject) != 0 || len(event.ResponseObject) != 0 {
			t.Fatal("fixture audit recorded content rather than metadata")
		}
		if event.Stage != "ResponseComplete" || event.Verb != "patch" || event.User.Username != "system:serviceaccount:fixture-allowed:scoped" || event.ObjectRef.Resource != "deployments" || event.ObjectRef.Name != "owned-deployment" {
			continue
		}
		if event.Level != "Metadata" || event.AuditID == "" || seen[event.AuditID] {
			t.Fatal("fixture audit patch identity was not unique metadata")
		}
		seen[event.AuditID] = true
		code := event.ResponseStatus.Code
		if (code == 200 || code == 409) && event.ObjectRef.Namespace == "fixture-allowed" || code == 403 && event.ObjectRef.Namespace == "fixture-denied" {
			counts[code]++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return counts
}
