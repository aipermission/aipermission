package api

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
)

func TestBestEffortAuditWriteReportsFailure(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	(&Server{}).writeAudit(context.Background(), nil, "user", nil, 17, "test.audit", map[string]any{
		"unsupported": func() {},
	})

	message := output.String()
	for _, expected := range []string{`actor="user"`, "runtime_id=17", `action="test.audit"`, "audit database is unavailable"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("audit failure log %q does not contain %q", message, expected)
		}
	}
}
