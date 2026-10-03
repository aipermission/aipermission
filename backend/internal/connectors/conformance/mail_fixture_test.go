package conformance_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	mailconnector "github.com/aipermission/aipermission/backend/internal/connectors/mail"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

func mailFixtureRuntime(mode string) connectors.RuntimeContext {
	imapPort, smtpPort := 993, 465
	if mode == "starttls" {
		imapPort, smtpPort = 143, 587
	}
	return connectors.RuntimeContext{
		Target: connectors.TargetView{
			ID: 8, Ref: "mail:8:8", ConnectorKind: mailconnector.Kind, Name: "conformance-mail",
			Config: map[string]any{
				"connection_mode": "direct", "imap_host": protocolFixtureHost, "imap_port": imapPort, "imap_tls_mode": mode,
				"smtp_host": protocolFixtureHost, "smtp_port": smtpPort, "smtp_tls_mode": mode, "allowed_recipient_domains": []string{"fixture.test"},
			},
		},
		Profile: connectors.CredentialProfileView{
			ID: 8, TargetID: 8, ConnectorKind: mailconnector.Kind, Kind: "password", Label: "conformance",
			Public: map[string]any{
				"mailbox_address": "recipient@fixture.test", "imap_enabled": true, "smtp_auth_mode": "reuse_imap",
				"allowed_read_folders":                 []string{"INBOX", "Archive", "Trash", "Sent"},
				"allowed_mutation_source_folders":      []string{"INBOX", "Archive", "Trash"},
				"allowed_mutation_destination_folders": []string{"Archive", "Trash"}, "archive_folder": "Archive", "trash_folder": "Trash",
			},
		},
		Secrets:      fixtureSecrets{"imap_username": "fixture-login", "imap_password": "conformance-only"},
		Capabilities: protocolCapabilities{requestedHost: protocolFixtureHost},
	}
}

func independentMailbox(t *testing.T) *client.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", "protocols:993")
	if err != nil {
		t.Fatal(err)
	}
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	t.Cleanup(func() { stop(); _ = raw.Close() })
	_ = raw.SetDeadline(time.Now().Add(20 * time.Second))
	secured := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: protocolFixtureHost})
	if err := secured.HandshakeContext(ctx); err != nil {
		t.Fatal(err)
	}
	mailbox, err := client.New(secured)
	if err != nil {
		t.Fatal(err)
	}
	mailbox.Timeout = 5 * time.Second
	if err := mailbox.Login("recipient", "conformance-only"); err != nil {
		t.Fatal(err)
	}
	return mailbox
}

func appendOwnedMessages(t *testing.T, mailbox *client.Client, marker string) {
	t.Helper()
	for index := 0; index < 3; index++ {
		data := fmt.Sprintf("From: sender@fixture.test\r\nTo: recipient@fixture.test\r\nSubject: %s-%d\r\nMessage-ID: <%s-%d@protocols>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nowned-body-%d", marker, index, marker, index, index)
		if err := mailbox.Append("INBOX", nil, time.Now(), bytes.NewReader([]byte(data))); err != nil {
			t.Fatal(err)
		}
	}
}

func assertIndependentSeenFlag(t *testing.T, mailbox *client.Client, ref map[string]any, want bool) {
	t.Helper()
	status, err := mailbox.Select("INBOX", true)
	if err != nil || status.UidValidity != ref["uidvalidity"] {
		t.Fatalf("independent mailbox generation: %#v / %v", status, err)
	}
	set := new(imap.SeqSet)
	set.AddNum(ref["uid"].(uint32))
	rows := make(chan *imap.Message, 1)
	if err := mailbox.UidFetch(set, []imap.FetchItem{imap.FetchUid, imap.FetchFlags}, rows); err != nil {
		t.Fatal(err)
	}
	row := <-rows
	if row == nil {
		t.Fatal("independent readback lost the message")
	}
	seen := false
	for _, flag := range row.Flags {
		seen = seen || flag == imap.SeenFlag
	}
	if seen != want {
		t.Fatalf("independent Seen state = %v, want %v", seen, want)
	}
}

func mailSearchPage(t *testing.T, runtime connectors.RuntimeContext, marker, cursor string) map[string]any {
	t.Helper()
	input := map[string]any{"folder": "INBOX", "subject": marker, "limit": 1}
	if cursor != "" {
		input["cursor"] = cursor
	}
	output := executeAction(t, mailconnector.New(), runtime, mailconnector.ActionSearchMessages, input).Output.(map[string]any)
	rows := output["messages"].([]map[string]any)
	if len(rows) != 1 || output["trust"] != "untrusted_external_content" {
		t.Fatalf("bounded Mail search contract: %#v", output)
	}
	return output
}
