package conformance_test

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

func invalidMailIdentityRuntime(mode, protocol, failure string) (connectors.RuntimeContext, connectors.TestStatus) {
	runtime := mailFixtureRuntime(mode)
	runtime.Profile.Public["smtp_auth_mode"] = "disabled"
	if protocol == "smtp" {
		runtime.Profile.Public["imap_enabled"] = false
		runtime.Profile.Public["smtp_auth_mode"] = "separate"
	}
	secrets := fixtureSecrets{protocol + "_username": "fixture-login", protocol + "_password": "conformance-only"}
	runtime.Secrets = secrets
	if failure == "password" {
		secrets[protocol+"_password"] = "not-the-fixture-password"
		return runtime, connectors.TestFailedAuth
	}
	runtime.Target.Config[protocol+"_host"] = "wrong-identity.invalid"
	runtime.Capabilities = protocolCapabilities{requestedHost: "wrong-identity.invalid"}
	return runtime, connectors.TestFailedTLS
}

func independentUIDs(t *testing.T, mailbox *client.Client, folder, header, value string) []uint32 {
	t.Helper()
	if _, err := mailbox.Select(folder, true); err != nil {
		t.Fatal(err)
	}
	criteria := imap.NewSearchCriteria()
	criteria.Header.Set(header, value)
	uids, err := mailbox.UidSearch(criteria)
	if err != nil {
		t.Fatal(err)
	}
	return uids
}

func fetchIndependentMessage(t *testing.T, mailbox *client.Client, uid uint32) (*imap.Message, []byte) {
	t.Helper()
	set := new(imap.SeqSet)
	set.AddNum(uid)
	section := &imap.BodySectionName{Peek: true}
	rows := make(chan *imap.Message, 1)
	if err := mailbox.UidFetch(set, []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchFlags, section.FetchItem()}, rows); err != nil {
		t.Fatal(err)
	}
	message := <-rows
	if message == nil || message.GetBody(section) == nil {
		t.Fatal("independent mailbox readback lost the owned message")
	}
	data, err := io.ReadAll(io.LimitReader(message.GetBody(section), 4097))
	if err != nil || len(data) > 4096 {
		t.Fatalf("independent owned message exceeded the fixture bound: %v", err)
	}
	return message, data
}

func appendDeletedSentinel(t *testing.T, mailbox *client.Client, marker string) string {
	t.Helper()
	id := "<sentinel-" + marker + "@protocols>"
	data := "From: sender@fixture.test\r\nTo: recipient@fixture.test\r\nSubject: unrelated-sentinel\r\nMessage-ID: " + id + "\r\n\r\npreserve-me"
	if err := mailbox.Append("INBOX", []string{imap.DeletedFlag}, time.Now(), bytes.NewReader([]byte(data))); err != nil {
		t.Fatal(err)
	}
	return id
}

func assertCompleteMailContinuation(t *testing.T, runtime connectors.RuntimeContext, mailbox *client.Client, marker string, first map[string]any) {
	t.Helper()
	expected := independentUIDs(t, mailbox, "INBOX", "Subject", marker)
	status := mailbox.Mailbox()
	if len(expected) != 3 {
		t.Fatalf("owned Mail setup did not create three messages: %v", expected)
	}
	observed := []uint32{}
	page := first
	for index := 0; index < 3; index++ {
		ref := page["messages"].([]map[string]any)[0]["message_ref"].(map[string]any)
		if ref["folder"] != "INBOX" || ref["uidvalidity"] != status.UidValidity {
			t.Fatal("Mail continuation changed independently observed mailbox generation")
		}
		uid := ref["uid"].(uint32)
		if uid != expected[2-index] || slices.Contains(observed, uid) {
			t.Fatalf("Mail continuation lost descending exact UID identity: %v / %v", observed, expected)
		}
		observed = append(observed, uid)
		cursor, _ := page["next_cursor"].(string)
		if index == 2 {
			if page["has_more"] != false || cursor != "" {
				t.Fatal("Mail continuation did not terminate after the exact owned result set")
			}
		} else {
			if page["has_more"] != true || cursor == "" {
				t.Fatal("Mail continuation omitted remaining owned results")
			}
			page = mailSearchPage(t, runtime, marker, cursor)
		}
	}
}

func assertIndependentArchive(t *testing.T, mailbox *client.Client, marker, sentinelID string, ref map[string]any) {
	t.Helper()
	remaining := independentUIDs(t, mailbox, "INBOX", "Subject", marker)
	if len(remaining) != 2 || slices.Contains(remaining, ref["uid"].(uint32)) {
		t.Fatalf("UID MOVE removed neighboring messages or retained the source: %v", remaining)
	}
	sentinel := independentUIDs(t, mailbox, "INBOX", "Message-ID", sentinelID)
	if len(sentinel) != 1 {
		t.Fatal("UID MOVE expunged an unrelated Deleted sentinel")
	}
	message, _ := fetchIndependentMessage(t, mailbox, sentinel[0])
	if !slices.Contains(message.Flags, imap.DeletedFlag) {
		t.Fatal("UID MOVE changed the unrelated Deleted sentinel")
	}
	moved := independentUIDs(t, mailbox, "Archive", "Message-ID", fmt.Sprintf("<%s-2@protocols>", marker))
	if len(moved) != 1 {
		t.Fatal("UID MOVE did not place the exact owned message in Archive")
	}
	message, body := fetchIndependentMessage(t, mailbox, moved[0])
	if message.Envelope.Subject != marker+"-2" || !strings.Contains(string(body), "owned-body-2") {
		t.Fatal("UID MOVE changed the archived owned content")
	}
}
