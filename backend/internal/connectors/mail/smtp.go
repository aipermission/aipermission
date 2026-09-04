package mailconnector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	stdmail "net/mail"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/emersion/go-imap"
	messagemail "github.com/emersion/go-message/mail"
	"github.com/emersion/go-smtp"
)

const maxReferences = 20

type smtpSubmissionClient interface {
	Mail(from string, opts *smtp.MailOptions) error
	Rcpt(to string, opts *smtp.RcptOptions) error
	Reset() error
	Data() (smtpDataWriter, error)
}

type smtpDataWriter interface {
	io.Writer
	CloseWithResponse() (*smtp.DataResponse, error)
}

type smtpClientAdapter struct{ client *smtp.Client }

func (adapter smtpClientAdapter) Mail(from string, opts *smtp.MailOptions) error {
	return adapter.client.Mail(from, opts)
}

func (adapter smtpClientAdapter) Rcpt(to string, opts *smtp.RcptOptions) error {
	return adapter.client.Rcpt(to, opts)
}

func (adapter smtpClientAdapter) Reset() error { return adapter.client.Reset() }

func (adapter smtpClientAdapter) Data() (smtpDataWriter, error) { return adapter.client.Data() }

type threadingHeaders struct {
	MessageID  string
	References []string
}

func executeOutboundAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, smtpActionTimeout)
	defer cancel()
	target, err := targetConfigFrom(runtime.Target)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	profile, err := profileConfigFrom(runtime.Profile)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if profile.SMTPAuthMode == "disabled" {
		return connectors.ActionResult{}, fmt.Errorf("%w: SMTP is disabled for this profile", ErrInvalidConfig)
	}
	outbound, _, err := normalizeOutbound(action.ActionName, action.Payload, target, profile)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	smtpSecrets, err := loadSMTPSecrets(ctx, runtime, profile)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	var threading threadingHeaders
	if action.ActionName == ActionReplyMessage {
		imapSecrets, secretErr := loadIMAPSecrets(ctx, runtime)
		if secretErr != nil {
			return connectors.ActionResult{}, secretErr
		}
		threading, err = loadThreadingHeaders(ctx, runtime, target, profile, imapSecrets, outbound)
		if err != nil {
			return connectors.ActionResult{}, err
		}
	}
	messageData, messageID, err := buildMessage(profile, outbound, threading, time.Now().UTC())
	if err != nil {
		return connectors.ActionResult{}, err
	}
	smtpClient, err := openSMTP(ctx, runtime, target, profile, smtpSecrets)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	defer func() { closeSMTP(smtpClient) }()
	result := submitSMTPMessage(smtpClientAdapter{client: smtpClient}, profile.MailboxAddress, outbound, messageData, messageID)
	if smtpSubmissionStatus(result) == "data_write_failed" {
		_ = smtpClient.Close()
		smtpClient = nil
	}
	if result.Output != nil {
		if err := validateResultSize(result.Output); err != nil {
			return connectors.ActionResult{}, err
		}
	}
	return result, nil
}

func smtpSubmissionStatus(result connectors.ActionResult) string {
	output, _ := result.Output.(map[string]any)
	return stringValue(output, "submission_status")
}

func loadThreadingHeaders(ctx context.Context, runtime connectors.RuntimeContext, target targetConfig, profile profileConfig, secrets protocolSecrets, outbound outboundMessage) (threadingHeaders, error) {
	if outbound.SourceRef == nil {
		return threadingHeaders{}, fmt.Errorf("reply source message reference is required")
	}
	imapClient, err := openIMAP(ctx, runtime, target, profile, secrets)
	if err != nil {
		return threadingHeaders{}, err
	}
	defer closeIMAP(imapClient)
	setIMAPTimeout(ctx, imapClient)
	status, err := imapClient.Select(outbound.SourceRef.Folder, true)
	if err != nil {
		return threadingHeaders{}, classifyProtocolError("IMAP EXAMINE", err)
	}
	if err := requireUIDValidity(status, outbound.SourceRef.UIDValidity, "reply source reference"); err != nil {
		return threadingHeaders{}, err
	}
	section := &imap.BodySectionName{BodyPartName: imap.BodyPartName{Specifier: imap.HeaderSpecifier, Fields: []string{"Message-ID", "In-Reply-To", "References"}}, Peek: true, Partial: []int{0, maxThreadingHeaderBytes + 1}}
	message, err := fetchOneMessage(ctx, imapClient, outbound.SourceRef.UID, []imap.FetchItem{imap.FetchUid, section.FetchItem()}, section)
	if err != nil {
		return threadingHeaders{}, err
	}
	if message == nil || message.GetBody(section) == nil {
		return threadingHeaders{}, connectors.ClassifyError("stale_message_reference", fmt.Errorf("reply source headers were not found"))
	}
	return parseThreadingHeaders(message.GetBody(section))
}

func parseThreadingHeaders(reader io.Reader) (threadingHeaders, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maxThreadingHeaderBytes+1))
	if err != nil {
		return threadingHeaders{}, fmt.Errorf("read reply source headers: %w", err)
	}
	if len(raw) > maxThreadingHeaderBytes {
		return threadingHeaders{}, fmt.Errorf("reply source headers exceed %d bytes", maxThreadingHeaderBytes)
	}
	message, err := stdmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return threadingHeaders{}, fmt.Errorf("parse reply source headers: %w", err)
	}
	messageID := normalizeMessageID(message.Header.Get("Message-ID"))
	if messageID == "" {
		return threadingHeaders{}, fmt.Errorf("reply source has no valid Message-ID")
	}
	references := validMessageIDs(message.Header.Get("References"))
	if inReplyTo := normalizeMessageID(message.Header.Get("In-Reply-To")); inReplyTo != "" {
		references = append(references, inReplyTo)
	}
	references = append(references, messageID)
	references = dedupeStrings(references)
	if len(references) > maxReferences {
		references = references[len(references)-maxReferences:]
	}
	return threadingHeaders{MessageID: messageID, References: references}, nil
}

func buildMessage(profile profileConfig, outbound outboundMessage, threading threadingHeaders, now time.Time) ([]byte, string, error) {
	from, err := parseMailboxAddress(profile.MailboxAddress)
	if err != nil {
		return nil, "", err
	}
	from.Name = profile.DisplayName
	var header messagemail.Header
	header.SetDate(now)
	header.SetAddressList("From", []*messagemail.Address{from})
	header.SetAddressList("To", outbound.To)
	if len(outbound.CC) > 0 {
		header.SetAddressList("Cc", outbound.CC)
	}
	if profile.ReplyTo != "" {
		replyTo, err := parseMailboxAddress(profile.ReplyTo)
		if err != nil {
			return nil, "", err
		}
		header.SetAddressList("Reply-To", []*messagemail.Address{replyTo})
	}
	header.SetSubject(outbound.Subject)
	_, fromDomain, _ := strings.Cut(from.Address, "@")
	if err := header.GenerateMessageIDWithHostname(fromDomain); err != nil {
		return nil, "", fmt.Errorf("generate Message-ID: %w", err)
	}
	rawMessageID, err := header.MessageID()
	messageID := normalizeMessageID("<" + rawMessageID + ">")
	if err != nil || messageID == "" {
		return nil, "", fmt.Errorf("generate valid Message-ID")
	}
	if threading.MessageID != "" {
		header.SetMsgIDList("In-Reply-To", []string{strings.Trim(threading.MessageID, "<>")})
		references := make([]string, 0, len(threading.References))
		for _, reference := range threading.References {
			references = append(references, strings.Trim(reference, "<>"))
		}
		header.SetMsgIDList("References", references)
	}
	var buffer bytes.Buffer
	writer, err := messagemail.CreateWriter(&buffer, header)
	if err != nil {
		return nil, "", fmt.Errorf("create mail message: %w", err)
	}
	if outbound.HTMLBody == "" {
		var inlineHeader messagemail.InlineHeader
		inlineHeader.Set("Content-Type", "text/plain; charset=utf-8")
		part, err := writer.CreateSingleInline(inlineHeader)
		if err != nil {
			return nil, "", fmt.Errorf("create plain-text message: %w", err)
		}
		if _, err := io.WriteString(part, outbound.TextBody); err != nil {
			return nil, "", fmt.Errorf("write plain-text message: %w", err)
		}
		if err := part.Close(); err != nil {
			return nil, "", fmt.Errorf("close plain-text message: %w", err)
		}
	} else {
		inline, err := writer.CreateInline()
		if err != nil {
			return nil, "", fmt.Errorf("create multipart alternative: %w", err)
		}
		for _, part := range []struct{ contentType, body string }{{"text/plain; charset=utf-8", outbound.TextBody}, {"text/html; charset=utf-8", outbound.HTMLBody}} {
			var inlineHeader messagemail.InlineHeader
			inlineHeader.Set("Content-Type", part.contentType)
			partWriter, err := inline.CreatePart(inlineHeader)
			if err != nil {
				return nil, "", fmt.Errorf("create message alternative: %w", err)
			}
			if _, err := io.WriteString(partWriter, part.body); err != nil {
				return nil, "", fmt.Errorf("write message alternative: %w", err)
			}
			if err := partWriter.Close(); err != nil {
				return nil, "", fmt.Errorf("close message alternative: %w", err)
			}
		}
		if err := inline.Close(); err != nil {
			return nil, "", fmt.Errorf("close multipart alternative: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close mail message: %w", err)
	}
	return buffer.Bytes(), messageID, nil
}

func submitSMTPMessage(client smtpSubmissionClient, from string, outbound outboundMessage, data []byte, messageID string) connectors.ActionResult {
	fromAddress, err := parseMailboxAddress(from)
	if err != nil {
		return connectors.ActionResult{Status: connectors.ResultFailed, Error: "SMTP envelope sender is invalid"}
	}
	if err := client.Mail(fromAddress.Address, nil); err != nil {
		return smtpFailed("mail_from_rejected", messageID, false, true, "SMTP rejected the envelope sender before message content was sent")
	}
	recipients := append(append(append([]*stdmail.Address{}, outbound.To...), outbound.CC...), outbound.BCC...)
	for index, recipient := range recipients {
		if err := client.Rcpt(recipient.Address, nil); err != nil {
			_ = client.Reset()
			result := smtpFailed("recipient_rejected", messageID, false, true, "SMTP rejected one or more recipients before message content was sent")
			output := result.Output.(map[string]any)
			output["rejected_recipient_index"] = index
			output["recipient_count"] = len(recipients)
			return result
		}
	}
	command, err := client.Data()
	if err != nil {
		_ = client.Reset()
		return smtpFailed("data_not_started", messageID, false, true, "SMTP refused message content before submission started")
	}
	if count, err := command.Write(data); err != nil || count != len(data) {
		// Do not close the DATA writer here: closing may terminate and submit a
		// truncated message. The caller drops the SMTP connection on return.
		return smtpFailed("data_write_failed", messageID, count > 0, false, "SMTP connection failed before the complete message was submitted")
	}
	response, err := command.CloseWithResponse()
	if err != nil {
		var smtpErr *smtp.SMTPError
		if errors.As(err, &smtpErr) {
			return smtpFailed("submission_rejected", messageID, true, true, "SMTP rejected the complete message")
		}
		return connectors.ActionResult{
			Status:      connectors.ResultOutcomeUnknown,
			Output:      map[string]any{"submission_status": "submission_unknown", "message_id": messageID, "message_content_transmitted": true, "retry_safe": false},
			DisplayText: "SMTP submission result is unknown. Inspect Sent/server state before any manual retry.",
			Error:       "SMTP connection ended after final submission; the server may have accepted the message. Do not automatically retry.",
		}
	}
	statusText := ""
	if response != nil {
		statusText = boundedText(response.StatusText, 1000)
	}
	output := map[string]any{"submission_status": "accepted", "message_id": messageID, "recipient_count": len(recipients), "message_content_transmitted": true, "server_status": statusText, "delivery_guaranteed": false}
	addUntrustedContentMetadata(output)
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: "SMTP accepted the message for delivery. Acceptance does not guarantee delivery.",
	}
}

func smtpFailed(classification, messageID string, contentTransmitted, retrySafe bool, message string) connectors.ActionResult {
	return connectors.ActionResult{Status: connectors.ResultFailed, Output: map[string]any{"submission_status": classification, "message_id": messageID, "message_content_transmitted": contentTransmitted, "retry_safe": retrySafe}, Error: message}
}

func normalizeMessageID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 5 || len(value) > 998 || value[0] != '<' || value[len(value)-1] != '>' || strings.ContainsAny(value, "\r\n\t ") {
		return ""
	}
	inner := value[1 : len(value)-1]
	if !strings.Contains(inner, "@") || strings.ContainsAny(inner, "<>") {
		return ""
	}
	return value
}

func validMessageIDs(value string) []string {
	parts := strings.Fields(value)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if normalized := normalizeMessageID(part); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

func dedupeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
