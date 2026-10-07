package console

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/console/persistence"
	"github.com/aipermission/aipermission/backend/internal/console/terminaltext"
)

func TestTranscriptTailReadsSmallPersistedChunksToByteBudget(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	var output strings.Builder
	for i := 0; i < 40; i++ {
		chunk := fmt.Sprintf("line-%02d-\u03bb\n", i)
		output.WriteString(chunk)
		if err := persistence.PersistTranscript(t.Context(), database, session.id, "short snapshot", chunk, time.Now().UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO console_session_chunks (session_id, seq, data, created_at) VALUES (?, 41, '', ?)`, session.id, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{1, 9, 137, maxConsoleTranscriptLength} {
		got := manager.transcriptTail(t.Context(), session.id, limit, "fallback")
		want := terminaltext.TailStringByBytes(output.String(), limit)
		if got != want || len(got) > limit || !utf8.ValidString(got) {
			t.Fatalf("limit %d: got %q, want %q", limit, got, want)
		}
	}
	record, err := manager.Get(t.Context(), session.id)
	if err != nil || record.Transcript != output.String() {
		t.Fatalf("Get transcript %q, error %v", record.Transcript, err)
	}
	// A missing session has no chunks and retains the existing snapshot fallback.
	if got := manager.transcriptTail(t.Context(), session.id+1, 4, "fallback"); got != "back" {
		t.Fatalf("missing-session fallback: %q", got)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if got := manager.transcriptTail(canceled, session.id, 4, "fallback"); got != "back" {
		t.Fatalf("canceled-read fallback: %q", got)
	}
}
