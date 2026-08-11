package auditoutbox_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/auditoutbox"
	"github.com/aipermission/aipermission/backend/internal/db"
)

func TestAppendPersistsCanonicalEvent(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "audit.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	event, err := (auditoutbox.Store{}).Append(context.Background(), database, auditoutbox.Event{
		ActorType:   "user",
		Action:      "project.created",
		PayloadJSON: `{"project_id":1}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(event.EventID) != 32 || event.EventVersion != auditoutbox.EventVersion {
		t.Fatalf("unexpected event identity: %#v", event)
	}

	var eventID, action, payload string
	if err := database.QueryRow(`SELECT event_id, action, payload_json FROM audit_outbox`).Scan(&eventID, &action, &payload); err != nil {
		t.Fatal(err)
	}
	if eventID != event.EventID || action != "project.created" || payload != `{"project_id":1}` {
		t.Fatalf("unexpected persisted event: %q %q %q", eventID, action, payload)
	}
}

func TestAppendRejectsInvalidAndOversizedPayloads(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "audit.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	store := auditoutbox.Store{}
	for name, payload := range map[string]string{
		"invalid":   `{`,
		"oversized": `{"value":"` + strings.Repeat("x", auditoutbox.MaxPayloadBytes) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Append(context.Background(), database, auditoutbox.Event{ActorType: "user", Action: "test", PayloadJSON: payload}); err == nil {
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
	if _, err := (auditoutbox.Store{}).Append(context.Background(), tx, auditoutbox.Event{ActorType: "user", Action: "test", PayloadJSON: `{}`}); err != nil {
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
