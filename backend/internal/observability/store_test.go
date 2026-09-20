package observability_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/timeformat"
)

func TestAppendPersistsCanonicalEvent(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "audit.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	event, err := (observability.Store{}).Append(context.Background(), database, observability.Event{
		ActorType:   "user",
		Action:      "project.created",
		PayloadJSON: `{"project_id":1}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(event.EventID) != 32 || event.EventVersion != observability.EventVersion {
		t.Fatalf("unexpected event identity: %#v", event)
	}

	var eventID, action, payload, occurredAt, createdAt string
	if err := database.QueryRow(`SELECT event_id, action, payload_json, occurred_at, created_at FROM audit_outbox`).Scan(&eventID, &action, &payload, &occurredAt, &createdAt); err != nil {
		t.Fatal(err)
	}
	if eventID != event.EventID || action != "project.created" || payload != `{"project_id":1}` {
		t.Fatalf("unexpected persisted event: %q %q %q", eventID, action, payload)
	}
	for _, value := range []string{occurredAt, createdAt} {
		if len(value) != len(timeformat.UTC(event.OccurredAt)) {
			t.Fatalf("audit timestamp is not fixed-width: %q", value)
		}
	}
}

func TestAppendRejectsInvalidAndOversizedPayloads(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "audit.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	store := observability.Store{}
	for name, payload := range map[string]string{
		"invalid":   `{`,
		"oversized": `{"value":"` + strings.Repeat("x", observability.MaxPayloadBytes) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Append(context.Background(), database, observability.Event{ActorType: "user", Action: "test", PayloadJSON: payload}); err == nil {
				t.Fatal("expected payload rejection")
			}
		})
	}
}

func TestAppendRollsBackWithCallerTransaction(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "audit.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (observability.Store{}).Append(context.Background(), tx, observability.Event{ActorType: "user", Action: "test", PayloadJSON: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back outbox rows = %d", count)
	}
}
