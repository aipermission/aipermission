package mailconnector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/connectortest"
	"github.com/emersion/go-imap"
)

func TestActionCatalogIsStableAndRiskClassified(t *testing.T) {
	actions, err := Connector{}.GetActionList(context.Background(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatalf("get action list: %v", err)
	}
	got := make([]string, 0, len(actions))
	risks := map[string]connectors.RiskLevel{}
	for _, action := range actions {
		got = append(got, action.Name)
		risks[action.Name] = action.Risk
	}
	want := []string{
		ActionListFolders, ActionCheckMailbox, ActionSearchMessages, ActionGetMessage,
		ActionListAttachments, ActionMarkRead, ActionMarkUnread, ActionMoveMessage,
		ActionArchiveMessage, ActionSendMessage, ActionReplyMessage, ActionDeleteMessage,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %#v, want %#v", got, want)
	}
	for _, action := range []string{ActionListFolders, ActionCheckMailbox, ActionSearchMessages, ActionGetMessage, ActionListAttachments} {
		if risks[action] != connectors.RiskRead {
			t.Fatalf("%s risk = %q", action, risks[action])
		}
	}
	for _, action := range []string{ActionMarkRead, ActionMarkUnread, ActionMoveMessage, ActionArchiveMessage, ActionSendMessage, ActionReplyMessage} {
		if risks[action] != connectors.RiskWrite {
			t.Fatalf("%s risk = %q", action, risks[action])
		}
	}
	if risks[ActionDeleteMessage] != connectors.RiskDestructive {
		t.Fatalf("delete risk = %q", risks[ActionDeleteMessage])
	}
	if err := connectors.ValidateActionDefinitions(actions, "mail actions"); err != nil {
		t.Fatalf("validate action definitions: %v", err)
	}
	for _, action := range actions {
		if action.Name != ActionCheckMailbox {
			continue
		}
		found := false
		for _, field := range action.InputSchema.Fields {
			if field.Name == "unread_only" && field.Default == true {
				found = true
			}
		}
		if !found {
			t.Fatal("check_mailbox must expose unread_only with a true default")
		}
	}
}

func TestActionCatalogSchemaSnapshot(t *testing.T) {
	actions, err := Connector{}.GetActionList(context.Background(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatalf("get action list: %v", err)
	}
	encoded, err := json.Marshal(actions)
	if err != nil {
		t.Fatalf("marshal action catalog: %v", err)
	}
	digest := sha256.Sum256(encoded)
	got := hex.EncodeToString(digest[:])
	const want = "966bdb09fad9f88f77224bdc9984055192c965a14e4e02f14ecc11dee16e8071"
	if got != want {
		t.Fatalf("action catalog schema changed: got sha256 %s, want %s; review the complete names, schemas, defaults, risks, and output hints before updating the snapshot", got, want)
	}
}

func TestPrepareReadActionEnforcesFolderPolicyAndIsDeterministic(t *testing.T) {
	req := connectors.ActionRequest{
		Target:     connectors.TargetView{ID: 1, Ref: "mail:1:2", ConnectorKind: Kind, Name: "Support", Config: validTargetConfig()},
		Profile:    connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Kind: "password", Label: "support", Public: validProfilePublic()},
		ActionName: ActionSearchMessages,
		Input:      map[string]any{"folder": "INBOX", "unread_only": true, "limit": 500},
	}
	prepared, err := Connector{}.PrepareAction(context.Background(), req)
	if err != nil {
		t.Fatalf("prepare search: %v", err)
	}
	if prepared.Payload["limit"] != maxMessageRows || prepared.Risk != connectors.RiskRead {
		t.Fatalf("prepared = %#v", prepared)
	}
	connectortest.AssertPrepareActionDeterministic(t, Connector{}, req)

	req.Input["folder"] = "Sent"
	_, err = Connector{}.PrepareAction(context.Background(), req)
	if !errors.Is(err, ErrFolderDenied) {
		t.Fatalf("expected folder denial, got %v", err)
	}
}

func TestPrepareMarkUnreadPreservesExactMessageReference(t *testing.T) {
	req := connectors.ActionRequest{
		Target:     connectors.TargetView{ID: 1, Ref: "mail:1:2", ConnectorKind: Kind, Config: validTargetConfig()},
		Profile:    connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Kind: "password", Public: validProfilePublic()},
		ActionName: ActionMarkUnread,
		Input:      map[string]any{"message_ref": map[string]any{"folder": "INBOX", "uidvalidity": 9, "uid": 42}},
	}
	prepared, err := Connector{}.PrepareAction(context.Background(), req)
	if err != nil {
		t.Fatalf("prepare mark unread: %v", err)
	}
	if prepared.Risk != connectors.RiskWrite || prepared.Preview["seen"] != false {
		t.Fatalf("prepared = %#v", prepared)
	}
	ref, err := parseMessageRef(prepared.Payload["message_ref"])
	if err != nil || ref.UIDValidity != 9 || ref.UID != 42 {
		t.Fatalf("message ref = %#v err=%v", ref, err)
	}
}

func TestPrepareRejectsFractionalMessageReferenceAndOversizedSearch(t *testing.T) {
	req := connectors.ActionRequest{
		Target:     connectors.TargetView{ID: 1, Ref: "mail:1:2", ConnectorKind: Kind, Config: validTargetConfig()},
		Profile:    connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Kind: "password", Public: validProfilePublic()},
		ActionName: ActionMarkRead,
		Input:      map[string]any{"message_ref": map[string]any{"folder": "INBOX", "uidvalidity": 9, "uid": 42.5}},
	}
	if _, err := (Connector{}).PrepareAction(context.Background(), req); err == nil {
		t.Fatal("expected fractional UID rejection")
	}
	req.ActionName = ActionSearchMessages
	req.Input = map[string]any{"folder": "INBOX", "sender": strings.Repeat("x", maxAddressBytes+1)}
	if _, err := (Connector{}).PrepareAction(context.Background(), req); err == nil || !strings.Contains(err.Error(), "sender search") {
		t.Fatalf("expected bounded sender search rejection, got %v", err)
	}
	action := connectors.PreparedAction{
		ActionName: ActionSearchMessages,
		Payload:    map[string]any{"folder": "INBOX", "sender": strings.Repeat("x", maxAddressBytes+1)},
	}
	if _, _, _, err := normalizedSearchFrom(action); err == nil || !strings.Contains(err.Error(), "sender search") {
		t.Fatalf("expected execute-layer sender search rejection, got %v", err)
	}
}

func TestPrepareSearchRejectsMalformedLimitAndUsesActionDefault(t *testing.T) {
	req := connectors.ActionRequest{
		Target:     connectors.TargetView{ID: 1, Ref: "mail:1:2", ConnectorKind: Kind, Config: validTargetConfig()},
		Profile:    connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Kind: "password", Public: validProfilePublic()},
		ActionName: ActionCheckMailbox,
		Input:      map[string]any{"folder": "INBOX"},
	}
	prepared, err := (Connector{}).PrepareAction(context.Background(), req)
	if err != nil {
		t.Fatalf("prepare default check: %v", err)
	}
	if prepared.Payload["limit"] != defaultMailboxRows {
		t.Fatalf("check_mailbox limit = %v, want %d", prepared.Payload["limit"], defaultMailboxRows)
	}
	for _, invalid := range []any{"not-a-number", 2.5} {
		req.Input["limit"] = invalid
		if _, err := (Connector{}).PrepareAction(context.Background(), req); err == nil || !strings.Contains(err.Error(), "limit") {
			t.Fatalf("limit %v: expected rejection, got %v", invalid, err)
		}
	}
}

func TestExecuteSearchNormalizationRejectsInvalidDates(t *testing.T) {
	_, _, _, err := normalizedSearchFrom(connectors.PreparedAction{
		ActionName: ActionSearchMessages,
		Payload:    map[string]any{"folder": "INBOX", "since": "not-a-date"},
	})
	if err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("expected invalid date rejection, got %v", err)
	}
}

func TestRecipientSearchCoversToAndCc(t *testing.T) {
	criteria, err := imapCriteria(normalizedSearch{Recipient: "operator@example.com"})
	if err != nil {
		t.Fatalf("imap criteria: %v", err)
	}
	if len(criteria.Or) != 1 || criteria.Or[0][0].Header.Get("To") != "operator@example.com" || criteria.Or[0][1].Header.Get("Cc") != "operator@example.com" {
		t.Fatalf("recipient criteria = %#v", criteria.Or)
	}
}

func TestPrepareArchiveFailsClosedWithoutDestination(t *testing.T) {
	public := validProfilePublic()
	public["archive_folder"] = ""
	req := connectors.ActionRequest{
		Target:     connectors.TargetView{ID: 1, Ref: "mail:1:2", ConnectorKind: Kind, Config: validTargetConfig()},
		Profile:    connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Kind: "password", Public: public},
		ActionName: ActionArchiveMessage,
		Input:      map[string]any{"message_ref": map[string]any{"folder": "INBOX", "uidvalidity": 9, "uid": 42}},
	}
	if _, err := (Connector{}).PrepareAction(t.Context(), req); err == nil || !strings.Contains(err.Error(), "destination is not configured") {
		t.Fatalf("expected missing archive destination rejection, got %v", err)
	}
}

func TestPrepareDeleteInTrashReturnsStableErrorCode(t *testing.T) {
	public := validProfilePublic()
	public["allowed_mutation_source_folders"] = []any{"INBOX", "Trash"}
	req := connectors.ActionRequest{
		Target:     connectors.TargetView{ID: 1, Ref: "mail:1:2", ConnectorKind: Kind, Config: validTargetConfig()},
		Profile:    connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Kind: "password", Public: public},
		ActionName: ActionDeleteMessage,
		Input:      map[string]any{"message_ref": map[string]any{"folder": "Trash", "uidvalidity": 9, "uid": 42}},
	}
	_, err := (Connector{}).PrepareAction(t.Context(), req)
	if connectors.ErrorCode(err) != "permanent_delete_unsupported" {
		t.Fatalf("error code = %q, err=%v", connectors.ErrorCode(err), err)
	}
}

func TestValidateCredentialProfileSupportsIMAPOnlyAndSeparateSMTP(t *testing.T) {
	connector := Connector{}
	if err := connector.ValidateCredentialProfile("password", validProfilePublic(), map[string]any{"imap_username": "support", "imap_password": "app-password"}, nil); err != nil {
		t.Fatalf("validate IMAP profile: %v", err)
	}
	smtpOnly := validProfilePublic()
	smtpOnly["imap_enabled"] = false
	smtpOnly["smtp_auth_mode"] = "separate"
	if err := connector.ValidateCredentialProfile("password", smtpOnly, map[string]any{"smtp_username": "support", "smtp_password": "app-password"}, nil); err != nil {
		t.Fatalf("validate SMTP profile: %v", err)
	}
	if err := connector.ValidateCredentialProfile("password", smtpOnly, map[string]any{}, nil); err == nil || !strings.Contains(err.Error(), "smtp_username") {
		t.Fatalf("expected separate SMTP secret error, got %v", err)
	}
}

func TestValidateTargetConfigRejectsSemanticProtocolErrors(t *testing.T) {
	config := validTargetConfig()
	config["imap_host"] = "https://mail.example.com"
	if err := (Connector{}).ValidateTargetConfig(config); err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("expected scheme-bearing host rejection, got %v", err)
	}
}

type fakeMailboxStatusClient struct {
	name   string
	items  []imap.StatusItem
	status *imap.MailboxStatus
	err    error
}

func (client *fakeMailboxStatusClient) Status(name string, items []imap.StatusItem) (*imap.MailboxStatus, error) {
	client.name = name
	client.items = append([]imap.StatusItem(nil), items...)
	return client.status, client.err
}

func TestMailboxUnreadCountUsesStatusUnseen(t *testing.T) {
	client := &fakeMailboxStatusClient{status: &imap.MailboxStatus{Unseen: 7}}
	unread, err := mailboxUnreadCount(context.Background(), client, "INBOX")
	if err != nil {
		t.Fatalf("mailbox unread count: %v", err)
	}
	if unread != 7 {
		t.Fatalf("unread = %d, want 7", unread)
	}
	if client.name != "INBOX" || !reflect.DeepEqual(client.items, []imap.StatusItem{imap.StatusUnseen}) {
		t.Fatalf("STATUS request = name %q items %#v", client.name, client.items)
	}
}

func TestHTMLBodyProjectionReportsReturnedContentType(t *testing.T) {
	result := map[string]any{}
	setBodyProjectionMetadata(result, "text/html")
	if result["body_content_type"] != "text/plain" || result["body_source_content_type"] != "text/html" || result["body_projection"] != "html_to_text" {
		t.Fatalf("projection metadata = %#v", result)
	}
}

func TestValidateCredentialProfileRejectsRemovedRequiredSecrets(t *testing.T) {
	previous := &connectors.CredentialProfileView{Public: validProfilePublic()}
	if err := (Connector{}).ValidateCredentialProfile("password", validProfilePublic(), map[string]any{}, previous); err == nil || !strings.Contains(err.Error(), "imap_username") {
		t.Fatalf("expected removed IMAP secrets to fail validation, got %v", err)
	}
	separate := validProfilePublic()
	separate["smtp_auth_mode"] = "separate"
	if err := (Connector{}).ValidateCredentialProfile("password", separate, map[string]any{"imap_username": "support", "imap_password": "secret"}, previous); err == nil || !strings.Contains(err.Error(), "smtp_username") {
		t.Fatalf("expected removed SMTP secrets to fail validation, got %v", err)
	}
}

func TestValidateCredentialProfileRejectsUnsafeDisplayName(t *testing.T) {
	public := validProfilePublic()
	public["display_name"] = "Support\r\nBcc: attacker@example.com"
	if err := (Connector{}).ValidateCredentialProfile("password", public, map[string]any{
		"imap_username": "support",
		"imap_password": "app-password",
	}, nil); err == nil || !strings.Contains(err.Error(), "display_name") {
		t.Fatalf("expected unsafe display name rejection, got %v", err)
	}

	public["display_name"] = strings.Repeat("x", maxDisplayNameBytes+1)
	if err := (Connector{}).ValidateCredentialProfile("password", public, map[string]any{
		"imap_username": "support",
		"imap_password": "app-password",
	}, nil); err == nil || !strings.Contains(err.Error(), "display_name") {
		t.Fatalf("expected oversized display name rejection, got %v", err)
	}
}

func TestTargetConfigRejectsUnsafeHostsAndPlaintextModes(t *testing.T) {
	config := validTargetConfig()
	config["imap_host"] = "imap.example.com:993"
	if _, err := targetConfigFrom(connectors.TargetView{Config: config}); err == nil {
		t.Fatal("expected embedded port rejection")
	}
	config = validTargetConfig()
	config["smtp_tls_mode"] = "plaintext"
	if _, err := targetConfigFrom(connectors.TargetView{Config: config}); err == nil {
		t.Fatal("expected plaintext mode rejection")
	}
}

func TestTargetConfigNormalizesIDNAHostsAndRejectsFractionalPorts(t *testing.T) {
	config := validTargetConfig()
	config["imap_host"] = "b\u00fccher.example"
	parsed, err := targetConfigFrom(connectors.TargetView{Config: config})
	if err != nil {
		t.Fatalf("target config: %v", err)
	}
	if parsed.IMAPHost != "xn--bcher-kva.example" {
		t.Fatalf("normalized IMAP host = %q", parsed.IMAPHost)
	}

	config = validTargetConfig()
	config["imap_port"] = 993.5
	if _, err := targetConfigFrom(connectors.TargetView{Config: config}); err == nil || !strings.Contains(err.Error(), "imap_port") {
		t.Fatalf("expected fractional port rejection, got %v", err)
	}
}

func TestReadDisplayTextDoesNotDuplicateMailContent(t *testing.T) {
	display := readDisplayText("search_messages", map[string]any{"text": "private message body"})
	if display != "search_messages completed." || strings.Contains(display, "private") {
		t.Fatalf("display text = %q", display)
	}
}

func TestExactIntValueRejectsPlatformOverflow(t *testing.T) {
	overflow := "9223372036854775808"
	if strconv.IntSize == 32 {
		overflow = "2147483648"
	}
	if _, err := exactIntValue(map[string]any{"limit": overflow}, "limit", 0); err == nil {
		t.Fatalf("expected %d-bit integer overflow rejection", strconv.IntSize)
	}
}

func TestConfigurationBoundsPolicyListsAndValidatesSentFolder(t *testing.T) {
	target := validTargetConfig()
	domains := make([]any, maxRecipientDomains+1)
	for index := range domains {
		domains[index] = fmt.Sprintf("tenant-%d.example.com", index)
	}
	target["allowed_recipient_domains"] = domains
	if _, err := targetConfigFrom(connectors.TargetView{Config: target}); err == nil || !strings.Contains(err.Error(), "allowed_recipient_domains") {
		t.Fatalf("expected recipient-domain bound, got %v", err)
	}

	public := validProfilePublic()
	public["sent_folder"] = "Sent\nInjected"
	if _, err := profileConfigFrom(connectors.CredentialProfileView{Public: public}); err == nil || !strings.Contains(err.Error(), "sent_folder") {
		t.Fatalf("expected sent folder validation, got %v", err)
	}
}

func TestFolderPolicyIndexPreservesConfiguredOrder(t *testing.T) {
	allowed := []string{"Sent", "INBOX", "Archive"}
	for folder, want := range map[string]int{"Sent": 0, "inbox": 1, "Archive": 2, "Unknown": 3} {
		if got := folderPolicyIndex(folder, allowed); got != want {
			t.Fatalf("folderPolicyIndex(%q) = %d, want %d", folder, got, want)
		}
	}
}

func TestFolderNamesRejectControlCharactersBeforeRendering(t *testing.T) {
	for _, folder := range []string{"INBOX\rInjected", "Sent\nNext", "Archive\tHidden"} {
		if err := validateFolderName(folder); err == nil {
			t.Fatalf("folder %q should be rejected", folder)
		}
	}
}

func TestConfiguredSentFolderSuppliesMissingSpecialUseRole(t *testing.T) {
	mailbox := &imap.MailboxInfo{Name: "Sent Items"}
	if got := folderRole(mailbox, profileConfig{SentFolder: "Sent Items"}); got != "sent" {
		t.Fatalf("folder role = %q, want sent", got)
	}
}

func TestIMAPBeforeCriterionIncludesTheWholeBoundaryDayForExactPostFilter(t *testing.T) {
	criteria, err := imapCriteria(normalizedSearch{Before: "2026-08-02T10:30:00Z"})
	if err != nil {
		t.Fatalf("criteria: %v", err)
	}
	if got := criteria.Before.UTC().Format(time.RFC3339); got != "2026-08-03T00:00:00Z" {
		t.Fatalf("coarse IMAP BEFORE = %s", got)
	}
}

func TestSinglePartBodyFetchUsesTextSpecifier(t *testing.T) {
	section := textBodySection(bodyPart{Structure: &imap.BodyStructure{MIMEType: "text", MIMESubType: "plain"}})
	if got := string(section.FetchItem()); !strings.HasPrefix(got, "BODY.PEEK[TEXT]") {
		t.Fatalf("single-part body fetch = %q, want BODY.PEEK[TEXT]", got)
	}
	section = textBodySection(bodyPart{Path: []int{1}, Structure: &imap.BodyStructure{MIMEType: "text", MIMESubType: "plain"}})
	if got := string(section.FetchItem()); !strings.HasPrefix(got, "BODY.PEEK[1]") {
		t.Fatalf("multipart body fetch = %q, want BODY.PEEK[1]", got)
	}
}

func TestUntrustedContentMetadataIsStable(t *testing.T) {
	output := map[string]any{}
	addUntrustedContentMetadata(output)
	if output["trust"] != "untrusted_external_content" || output["warning"] != untrustedContentWarning {
		t.Fatalf("untrusted metadata = %#v", output)
	}
}

func validTargetConfig() map[string]any {
	return map[string]any{
		"connection_mode":           "direct",
		"imap_host":                 "imap.example.com",
		"imap_port":                 993,
		"imap_tls_mode":             "implicit_tls",
		"smtp_host":                 "smtp.example.com",
		"smtp_port":                 465,
		"smtp_tls_mode":             "implicit_tls",
		"allowed_recipient_domains": []any{"example.com"},
	}
}

func validProfilePublic() map[string]any {
	return map[string]any{
		"mailbox_address":                      "support@example.com",
		"imap_enabled":                         true,
		"smtp_auth_mode":                       "disabled",
		"allowed_read_folders":                 []any{"INBOX"},
		"allowed_mutation_source_folders":      []any{"INBOX"},
		"allowed_mutation_destination_folders": []any{"Archive", "Trash"},
		"archive_folder":                       "Archive",
		"trash_folder":                         "Trash",
	}
}
