package mailconnector

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	stdmail "net/mail"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/connectortest"
	"github.com/emersion/go-smtp"
)

type fakeSMTPClient struct {
	recipients []string
	mailErr    error
	rejectAt   int
	reset      bool
	data       *fakeSMTPData
	dataErr    error
}

func (client *fakeSMTPClient) Mail(string, *smtp.MailOptions) error { return client.mailErr }

func (client *fakeSMTPClient) Rcpt(recipient string, _ *smtp.RcptOptions) error {
	client.recipients = append(client.recipients, recipient)
	if client.rejectAt > 0 && len(client.recipients) == client.rejectAt {
		return errors.New("recipient refused")
	}
	return nil
}

func (client *fakeSMTPClient) Reset() error { client.reset = true; return nil }

func (client *fakeSMTPClient) Data() (smtpDataWriter, error) {
	if client.dataErr != nil {
		return nil, client.dataErr
	}
	if client.data == nil {
		client.data = &fakeSMTPData{response: &smtp.DataResponse{StatusText: "queued"}}
	}
	return client.data, nil
}

type fakeSMTPData struct {
	bytes.Buffer
	response   *smtp.DataResponse
	closeErr   error
	shortWrite bool
	writeErr   error
}

func (data *fakeSMTPData) Write(value []byte) (int, error) {
	if data.writeErr != nil {
		return 0, data.writeErr
	}
	if data.shortWrite && len(value) > 0 {
		return len(value) - 1, nil
	}
	return data.Buffer.Write(value)
}

func (data *fakeSMTPData) CloseWithResponse() (*smtp.DataResponse, error) {
	return data.response, data.closeErr
}

func TestPrepareOutboundShowsCompleteApprovalPreviewAndSanitizesHTML(t *testing.T) {
	profilePublic := validProfilePublic()
	profilePublic["smtp_auth_mode"] = "reuse_imap"
	request := connectors.ActionRequest{
		Target:     connectors.TargetView{ID: 1, Ref: "mail:1:2", ConnectorKind: Kind, Config: validTargetConfig()},
		Profile:    connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Public: profilePublic},
		ActionName: ActionSendMessage,
		Input: map[string]any{
			"to":        []any{"User <user@example.com>"},
			"bcc":       []any{"audit@example.com"},
			"subject":   "Status",
			"text_body": "Complete body",
			"html_body": `<p>Complete <strong>body</strong></p><p><a href="https://example.com/status">Status</a></p><img src="https://tracker.invalid/pixel"><script>bad()</script>`,
		},
	}
	prepared, err := Connector{}.PrepareAction(t.Context(), request)
	if err != nil {
		t.Fatalf("prepare send: %v", err)
	}
	if prepared.Preview["text_body"] != "Complete body" || !strings.Contains(prepared.Preview["formatted_text_body"].(string), "https://example.com/status") || prepared.Preview["formatted_text_matches_fallback"] != false {
		t.Fatalf("preview = %#v", prepared.Preview)
	}
	htmlBody := prepared.Payload["html_body"].(string)
	if strings.Contains(htmlBody, "img") || strings.Contains(htmlBody, "script") || strings.Contains(htmlBody, "tracker") {
		t.Fatalf("unsafe HTML survived: %q", htmlBody)
	}
	connectortest.AssertPrepareActionDeterministic(t, Connector{}, request)
}

func TestPrepareOutboundDoesNotDuplicateMatchingFormattedFallback(t *testing.T) {
	profilePublic := validProfilePublic()
	profilePublic["smtp_auth_mode"] = "reuse_imap"
	request := connectors.ActionRequest{
		Target:     connectors.TargetView{ID: 1, Ref: "mail:1:2", ConnectorKind: Kind, Config: validTargetConfig()},
		Profile:    connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Public: profilePublic},
		ActionName: ActionSendMessage,
		Input: map[string]any{
			"to":        []any{"user@example.com"},
			"subject":   "Status",
			"text_body": "Complete body",
			"html_body": "<p>Complete body</p>",
		},
	}
	prepared, err := (Connector{}).PrepareAction(t.Context(), request)
	if err != nil {
		t.Fatalf("prepare send: %v", err)
	}
	if prepared.Preview["formatted_text_matches_fallback"] != true {
		t.Fatalf("preview = %#v", prepared.Preview)
	}
	if _, duplicated := prepared.Preview["formatted_text_body"]; duplicated {
		t.Fatalf("matching formatted projection was duplicated: %#v", prepared.Preview)
	}
}

func TestPreparedOutboundPreviewRemainsInsideGenericEnvelope(t *testing.T) {
	profilePublic := validProfilePublic()
	profilePublic["smtp_auth_mode"] = "reuse_imap"
	request := connectors.ActionRequest{
		Target:     connectors.TargetView{ID: 1, Ref: "mail:1:2", ConnectorKind: Kind, Config: validTargetConfig()},
		Profile:    connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Public: profilePublic},
		ActionName: ActionSendMessage,
		Input: map[string]any{
			"to":        []any{"user@example.com"},
			"subject":   "Bounded preview",
			"text_body": strings.Repeat("\n", maxTextBodyBytes),
			"html_body": strings.Repeat("<br>", maxHTMLBodyBytes/4),
		},
	}
	prepared, err := (Connector{}).PrepareAction(t.Context(), request)
	if err != nil {
		t.Fatalf("prepare send: %v", err)
	}
	encoded, err := json.Marshal(prepared.Preview)
	if err != nil {
		t.Fatalf("marshal preview: %v", err)
	}
	if len(encoded) > 1<<20 {
		t.Fatalf("preview bytes = %d", len(encoded))
	}
}

func TestPrepareOutboundRejectsHTMLProjectionThatCannotBeFullyApproved(t *testing.T) {
	profilePublic := validProfilePublic()
	profilePublic["smtp_auth_mode"] = "reuse_imap"
	link := `<a href="https://example.com/x">x</a>`
	request := connectors.ActionRequest{
		Target:     connectors.TargetView{ID: 1, Ref: "mail:1:2", ConnectorKind: Kind, Config: validTargetConfig()},
		Profile:    connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: Kind, Public: profilePublic},
		ActionName: ActionSendMessage,
		Input: map[string]any{
			"to":        []any{"user@example.com"},
			"subject":   "Bounded approval",
			"text_body": "Fallback body",
			"html_body": strings.Repeat(link, maxHTMLBodyBytes/len(link)),
		},
	}
	_, err := (Connector{}).PrepareAction(t.Context(), request)
	if err == nil || !strings.Contains(err.Error(), "text projection exceeds") {
		t.Fatalf("expected fail-closed projection rejection, got %v", err)
	}
}

func TestNormalizeOutboundRejectsHeaderInjectionAndRecipientPolicy(t *testing.T) {
	target, _ := targetConfigFrom(connectors.TargetView{Config: validTargetConfig()})
	profile, _ := profileConfigFrom(connectors.CredentialProfileView{Public: validProfilePublic()})
	_, _, err := normalizeOutbound(ActionSendMessage, map[string]any{"to": []any{"user@example.com"}, "subject": "hello\r\nBcc: hidden@example.com", "text_body": "body"}, target, profile)
	if err == nil {
		t.Fatal("expected header injection rejection")
	}
	_, _, err = normalizeOutbound(ActionSendMessage, map[string]any{"to": []any{"user@outside.example"}, "subject": "hello", "text_body": "body"}, target, profile)
	if err == nil || !strings.Contains(err.Error(), "outside the target allowlist") {
		t.Fatalf("expected recipient policy rejection, got %v", err)
	}
}

func TestParseAddressInputAcceptsQuotedRFCAddressList(t *testing.T) {
	addresses, err := parseAddressInput(`"Doe, Jane" <jane@example.com>, operator@example.com`, []string{"example.com"})
	if err != nil {
		t.Fatalf("parse address list: %v", err)
	}
	if len(addresses) != 2 || addresses[0].Name != "Doe, Jane" || addresses[0].Address != "jane@example.com" {
		t.Fatalf("addresses = %#v", addresses)
	}
}

func TestParseAddressInputCarriesNormalizedInternationalDomain(t *testing.T) {
	addresses, err := parseAddressInput("Operator <operator@b\u00fccher.example>", []string{"xn--bcher-kva.example"})
	if err != nil {
		t.Fatalf("parse international address: %v", err)
	}
	if len(addresses) != 1 || addresses[0].Address != "operator@xn--bcher-kva.example" {
		t.Fatalf("addresses = %#v", addresses)
	}
}

func TestBuildMessageOmitsBCCHeaderAndCreatesMultipartAlternative(t *testing.T) {
	profile, _ := profileConfigFrom(connectors.CredentialProfileView{Public: validProfilePublic()})
	message, messageID, err := buildMessage(profile, outboundMessage{
		To:       []*stdmail.Address{{Address: "user@example.com"}},
		BCC:      []*stdmail.Address{{Address: "hidden@example.com"}},
		Subject:  "Hello",
		TextBody: "Plain body",
		HTMLBody: "<p>Formatted body</p>",
	}, threadingHeaders{}, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	if normalizeMessageID(messageID) == "" {
		t.Fatalf("message id = %q", messageID)
	}
	text := string(message)
	if strings.Contains(strings.ToLower(text), "bcc:") || strings.Contains(text, "hidden@example.com") {
		t.Fatalf("BCC leaked into message headers/body: %s", text)
	}
	if !strings.Contains(text, "multipart/alternative") || !strings.Contains(text, "text/plain") || !strings.Contains(text, "text/html") {
		t.Fatalf("multipart alternative missing: %s", text)
	}
}

func TestSubmitSMTPMessageResetsBeforeDataWhenAnyRecipientIsRejected(t *testing.T) {
	client := &fakeSMTPClient{rejectAt: 2}
	result := submitSMTPMessage(client, "support@example.com", outboundMessage{
		To: []*stdmail.Address{{Address: "one@example.com"}, {Address: "two@example.com"}},
	}, []byte("message"), "<id@example.com>")
	if result.Status != connectors.ResultFailed || !client.reset || client.data != nil {
		t.Fatalf("result=%#v reset=%v data=%#v", result, client.reset, client.data)
	}
	if result.Output.(map[string]any)["retry_safe"] != true {
		t.Fatalf("recipient rejection retry metadata = %#v", result.Output)
	}
}

func TestSubmitSMTPMessageClassifiesUnknownFinalResponseWithoutRetryHint(t *testing.T) {
	client := &fakeSMTPClient{data: &fakeSMTPData{closeErr: io.ErrUnexpectedEOF}}
	result := submitSMTPMessage(client, "support@example.com", outboundMessage{To: []*stdmail.Address{{Address: "one@example.com"}}}, []byte("message"), "<id@example.com>")
	if result.Status != connectors.ResultError || !strings.Contains(result.Error, "Do not automatically retry") {
		t.Fatalf("result = %#v", result)
	}
	output := result.Output.(map[string]any)
	if output["submission_status"] != "submission_unknown" || output["retry_safe"] != false {
		t.Fatalf("output = %#v", output)
	}
}

func TestSubmitSMTPMessageMarksOnlyUnambiguousPreDataFailuresRetrySafe(t *testing.T) {
	message := outboundMessage{To: []*stdmail.Address{{Address: "one@example.com"}}}
	for name, client := range map[string]*fakeSMTPClient{
		"mail": {mailErr: errors.New("rejected")},
		"data": {dataErr: errors.New("rejected")},
	} {
		result := submitSMTPMessage(client, "support@example.com", message, []byte("message"), "<id@example.com>")
		output := result.Output.(map[string]any)
		if result.Status != connectors.ResultFailed || output["retry_safe"] != true || output["message_content_transmitted"] != false {
			t.Fatalf("%s result = %#v", name, result)
		}
	}

	client := &fakeSMTPClient{data: &fakeSMTPData{closeErr: &smtp.SMTPError{Code: 554, Message: "rejected"}}}
	result := submitSMTPMessage(client, "support@example.com", message, []byte("message"), "<id@example.com>")
	if result.Output.(map[string]any)["retry_safe"] != true || result.Output.(map[string]any)["message_content_transmitted"] != true {
		t.Fatalf("explicit final rejection = %#v", result)
	}
}

func TestSubmitSMTPMessageRejectsShortDataWrites(t *testing.T) {
	client := &fakeSMTPClient{data: &fakeSMTPData{shortWrite: true}}
	result := submitSMTPMessage(client, "support@example.com", outboundMessage{To: []*stdmail.Address{{Address: "one@example.com"}}}, []byte("message"), "<id@example.com>")
	output := result.Output.(map[string]any)
	if result.Status != connectors.ResultFailed || output["submission_status"] != "data_write_failed" || output["retry_safe"] != false || output["message_content_transmitted"] != true {
		t.Fatalf("result = %#v", result)
	}

	client = &fakeSMTPClient{data: &fakeSMTPData{writeErr: io.ErrClosedPipe}}
	result = submitSMTPMessage(client, "support@example.com", outboundMessage{To: []*stdmail.Address{{Address: "one@example.com"}}}, []byte("message"), "<id@example.com>")
	output = result.Output.(map[string]any)
	if output["message_content_transmitted"] != false || output["retry_safe"] != false {
		t.Fatalf("zero-byte DATA failure metadata = %#v", output)
	}
}

func TestAcceptedSMTPStatusIsMarkedAsUntrustedServerContent(t *testing.T) {
	client := &fakeSMTPClient{}
	result := submitSMTPMessage(client, "support@example.com", outboundMessage{To: []*stdmail.Address{{Address: "one@example.com"}}}, []byte("message"), "<id@example.com>")
	output := result.Output.(map[string]any)
	if output["trust"] != "untrusted_external_content" || output["warning"] != untrustedContentWarning {
		t.Fatalf("output = %#v", output)
	}
}

func TestParseThreadingHeadersBoundsAndValidatesReferences(t *testing.T) {
	raw := "Message-ID: <source@example.com>\r\nReferences: bad <one@example.com> <two@example.com>\r\nIn-Reply-To: <two@example.com>\r\n\r\n"
	headers, err := parseThreadingHeaders(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("parse threading: %v", err)
	}
	if headers.MessageID != "<source@example.com>" || len(headers.References) != 3 {
		t.Fatalf("headers = %#v", headers)
	}
}

func TestParseThreadingHeadersRejectsTruncatedHeaderBlocks(t *testing.T) {
	raw := "Message-ID: <source@example.com>\r\nX-Padding: " + strings.Repeat("x", maxThreadingHeaderBytes) + "\r\n\r\n"
	if _, err := parseThreadingHeaders(strings.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("expected oversized threading header rejection, got %v", err)
	}
}

func FuzzSanitizeOutboundHTML(f *testing.F) {
	f.Add(`<p>Hello</p><img src=x onerror=alert(1)>`)
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > maxHTMLBodyBytes {
			t.Skip()
		}
		result := strings.ToLower(sanitizeOutboundHTML(source))
		for _, forbidden := range []string{"<script", "<img", "onerror=", "javascript:"} {
			if strings.Contains(result, forbidden) {
				t.Fatalf("unsafe output %q", result)
			}
		}
	})
}

func TestSanitizeOutboundHTMLPreservesContentEditableBlockBoundaries(t *testing.T) {
	sanitized := sanitizeOutboundHTML(`<div>First line</div><div>Second line</div>`)
	if !strings.Contains(sanitized, "<div>First line</div><div>Second line</div>") {
		t.Fatalf("contentEditable blocks were removed: %q", sanitized)
	}
	text, err := htmlToText(sanitized)
	if err != nil {
		t.Fatalf("project sanitized HTML: %v", err)
	}
	if !strings.Contains(text, "First line") || !strings.Contains(text, "Second line") || strings.Contains(text, "First lineSecond line") {
		t.Fatalf("plain-text projection = %q", text)
	}
}
