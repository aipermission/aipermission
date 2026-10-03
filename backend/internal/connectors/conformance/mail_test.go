package conformance_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	mailconnector "github.com/aipermission/aipermission/backend/internal/connectors/mail"
	"github.com/emersion/go-imap"
)

func TestMailRealService(t *testing.T) {
	requireProtocolFixture(t)
	for _, mode := range []string{"implicit_tls", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			runtime := mailFixtureRuntime(mode)
			connector := mailconnector.New()
			assertConnection(t, connector, runtime)
			folders := executeAction(t, connector, runtime, mailconnector.ActionListFolders, nil).Output.(map[string]any)["folders"].([]map[string]any)
			if len(folders) != 4 || folders[0]["name"] != "INBOX" || folders[1]["name"] != "Archive" || folders[2]["name"] != "Trash" || folders[3]["name"] != "Sent" {
				t.Fatalf("Dovecot folder policy order: %#v", folders)
			}
			mailbox := independentMailbox(t)
			marker := fmt.Sprintf("owned-%x", time.Now().UnixNano())
			appendOwnedMessages(t, mailbox, marker)
			sentinelID := appendDeletedSentinel(t, mailbox, marker)
			page := mailSearchPage(t, runtime, marker, "")
			assertCompleteMailContinuation(t, runtime, mailbox, marker, page)
			ref := page["messages"].([]map[string]any)[0]["message_ref"].(map[string]any)
			message := executeAction(t, connector, runtime, mailconnector.ActionGetMessage, map[string]any{"message_ref": ref})
			assertResultContains(t, message, "owned-body-2")
			assertIndependentSeenFlag(t, mailbox, ref, false)
			for _, mutation := range []struct {
				action string
				seen   bool
			}{{mailconnector.ActionMarkRead, true}, {mailconnector.ActionMarkUnread, false}} {
				executeAction(t, connector, runtime, mutation.action, map[string]any{"message_ref": ref})
				assertIndependentSeenFlag(t, mailbox, ref, mutation.seen)
			}
			executeAction(t, connector, runtime, mailconnector.ActionArchiveMessage, map[string]any{"message_ref": ref})
			assertIndependentArchive(t, mailbox, marker, sentinelID, ref)
			assertMailReferenceStale(t, runtime, ref)
			assertSMTPDelivered(t, runtime, marker)
		})
	}
}

func assertMailReferenceStale(t *testing.T, runtime connectors.RuntimeContext, ref map[string]any) {
	t.Helper()
	connector := mailconnector.New()
	prepared, err := connector.PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: mailconnector.ActionGetMessage, Input: map[string]any{"message_ref": ref}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = connector.ExecuteAction(t.Context(), runtime, prepared)
	if connectors.ErrorCode(err) != "stale_message_reference" {
		t.Fatalf("moved Dovecot UID was not classified stale: %v", err)
	}
}

func assertSMTPDelivered(t *testing.T, runtime connectors.RuntimeContext, marker string) {
	t.Helper()
	connector := mailconnector.New()
	result := executeAction(t, connector, runtime, mailconnector.ActionSendMessage, map[string]any{"to": []string{"recipient@fixture.test"}, "subject": "delivery-" + marker, "text_body": "owned-delivery-body"})
	output := result.Output.(map[string]any)
	if output["submission_status"] != "accepted" || output["delivery_guaranteed"] != false {
		t.Fatalf("SMTP acceptance was mislabeled: %#v", output)
	}
	mailbox := independentMailbox(t)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := mailbox.Select("INBOX", true); err != nil {
			t.Fatal(err)
		}
		criteria := imap.NewSearchCriteria()
		criteria.Header.Set("Message-ID", output["message_id"].(string))
		uids, err := mailbox.UidSearch(criteria)
		if err != nil {
			t.Fatal(err)
		}
		if len(uids) == 1 {
			message, data := fetchIndependentMessage(t, mailbox, uids[0])
			if message.Envelope == nil || message.Envelope.Subject != "delivery-"+marker || len(message.Envelope.To) != 1 || message.Envelope.To[0].Address() != "recipient@fixture.test" || !strings.Contains(string(data), "owned-delivery-body") {
				t.Fatalf("owned SMTP delivery changed envelope or body: %#v", message.Envelope)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("owned Postfix recipient delivery was not read back: %d matching messages", len(uids))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestMailRealServiceRejectsInvalidIdentity(t *testing.T) {
	requireProtocolFixture(t)
	connector := mailconnector.New()
	for _, mode := range []string{"implicit_tls", "starttls"} {
		for _, protocol := range []string{"imap", "smtp"} {
			for _, failure := range []string{"password", "hostname"} {
				t.Run(mode+"/"+protocol+"/"+failure, func(t *testing.T) {
					runtime, want := invalidMailIdentityRuntime(mode, protocol, failure)
					result, err := connector.TestConnection(t.Context(), runtime)
					if err != nil || result.Status != want {
						t.Fatalf("invalid %s %s was not refused: %#v / %v", protocol, failure, result, err)
					}
				})
			}
		}
	}
}
