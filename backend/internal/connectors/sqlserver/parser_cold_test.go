package sqlserverconnector

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExecutionParserColdStart(t *testing.T) {
	if os.Getenv("AIPERMISSION_PARSER_COLD_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExecutionParserColdStart$", "-test.v")
		cmd.Env = append(os.Environ(), "AIPERMISSION_PARSER_COLD_CHILD=1", "GOMAXPROCS=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cold parser: %v\n%s", err, out)
		} else {
			t.Logf("%s", out)
		}
		return
	}
	runtime.GOMAXPROCS(1)
	start := time.Now()
	New()
	initialized := time.Since(start)
	script := "DECLARE @n int = 1;\nSELECT @n;\nGO\nSELECT 2;"
	start = time.Now()
	batches, err := compileExecutionSQL(t.Context(), script)
	if err != nil || len(batches) != 2 {
		t.Fatalf("first valid script rejected: %v", err)
	}
	t.Logf("fixed grammar initialization=%s; first bounded parse=%s", initialized, time.Since(start))
}

func TestExecutionValidationDeadlineIsActionableAndWrapped(t *testing.T) {
	if executionParserTimeout != time.Second {
		t.Fatal("production parser admission budget changed")
	}
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	batches, err := compileExecutionSQL(ctx, "SELECT 1;")
	if len(batches) != 0 || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "T-SQL validation") || !strings.Contains(err.Error(), "no SQL was sent") {
		t.Fatalf("unhelpful validation timeout: %v", err)
	}
}
