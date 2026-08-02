package mailconnector

import (
	"fmt"
	"net/mail"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/microcosm-cc/bluemonday"
)

type outboundMessage struct {
	To             []*mail.Address
	CC             []*mail.Address
	BCC            []*mail.Address
	Subject        string
	TextBody       string
	HTMLBody       string
	HTMLNormalized bool
	SourceRef      *messageRef
}

var outboundHTMLPolicy = newOutboundHTMLPolicy()

func prepareOutboundAction(req connectors.ActionRequest, target targetConfig, profile profileConfig) (connectors.PreparedAction, error) {
	if profile.SMTPAuthMode == "disabled" {
		return connectors.PreparedAction{}, fmt.Errorf("%w: SMTP is disabled for this profile", ErrInvalidConfig)
	}
	outbound, payload, err := normalizeOutbound(req.ActionName, req.Input, target, profile)
	if err != nil {
		return connectors.PreparedAction{}, err
	}
	preview := map[string]any{
		"from":                 profile.MailboxAddress,
		"to":                   addressStrings(outbound.To),
		"cc":                   addressStrings(outbound.CC),
		"bcc":                  addressStrings(outbound.BCC),
		"subject":              outbound.Subject,
		"text_body":            outbound.TextBody,
		"has_html_alternative": outbound.HTMLBody != "",
		"sanitized_html_bytes": len(outbound.HTMLBody),
	}
	if outbound.HTMLBody != "" {
		projection, err := htmlToText(outbound.HTMLBody)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		if len(projection) > maxTextBodyBytes {
			return connectors.PreparedAction{}, fmt.Errorf("formatted message text projection exceeds %d bytes; shorten the HTML before approval", maxTextBodyBytes)
		}
		matchesFallback := normalizeBodyWhitespace(projection) == normalizeBodyWhitespace(outbound.TextBody)
		preview["formatted_text_matches_fallback"] = matchesFallback
		if !matchesFallback {
			preview["formatted_text_body"] = projection
		}
	}
	warnings := []string{"SMTP acceptance is not delivery. Do not automatically retry an unknown submission."}
	if outbound.HTMLNormalized {
		warnings = append(warnings, "Formatted content was normalized to the supported safe HTML subset.")
	}
	preview["warnings"] = warnings
	title := "Send mail message"
	summary := fmt.Sprintf("Send one message from %s to %d recipient(s).", profile.MailboxAddress, len(outbound.To)+len(outbound.CC)+len(outbound.BCC))
	if req.ActionName == ActionReplyMessage {
		title = "Reply to mail message"
		summary = fmt.Sprintf("Reply from %s to %d explicit recipient(s).", profile.MailboxAddress, len(outbound.To)+len(outbound.CC)+len(outbound.BCC))
		preview["source_message_ref"] = outbound.SourceRef.mapValue()
	}
	return connectors.PreparedAction{
		ConnectorKind:   Kind,
		TargetRef:       req.Target.Ref,
		ProfileID:       req.Profile.ID,
		ActionName:      req.ActionName,
		Risk:            connectors.RiskWrite,
		Title:           title,
		Summary:         summary,
		Preview:         preview,
		Payload:         payload,
		ContextMaterial: contextMaterial(target, profile),
	}, nil
}

func normalizeOutbound(actionName string, input map[string]any, target targetConfig, profile profileConfig) (outboundMessage, map[string]any, error) {
	if actionName != ActionSendMessage && actionName != ActionReplyMessage {
		return outboundMessage{}, nil, ErrUnsupportedAction
	}
	to, err := parseAddressInput(input["to"], target.AllowedRecipientDomains)
	if err != nil {
		return outboundMessage{}, nil, fmt.Errorf("to: %w", err)
	}
	if len(to) == 0 {
		return outboundMessage{}, nil, fmt.Errorf("at least one To recipient is required")
	}
	cc, err := parseAddressInput(input["cc"], target.AllowedRecipientDomains)
	if err != nil {
		return outboundMessage{}, nil, fmt.Errorf("cc: %w", err)
	}
	bcc, err := parseAddressInput(input["bcc"], target.AllowedRecipientDomains)
	if err != nil {
		return outboundMessage{}, nil, fmt.Errorf("bcc: %w", err)
	}
	to, cc, bcc = dedupeAddressGroups(to, cc, bcc)
	if len(to)+len(cc)+len(bcc) > maxRecipients {
		return outboundMessage{}, nil, fmt.Errorf("recipient count exceeds %d", maxRecipients)
	}
	subject := strings.TrimSpace(rawStringValue(input, "subject"))
	if subject == "" || strings.ContainsAny(subject, "\r\n") || len(subject) > maxSubjectBytes {
		return outboundMessage{}, nil, fmt.Errorf("subject is required, must be one line, and must not exceed %d bytes", maxSubjectBytes)
	}
	if actionName == ActionReplyMessage && !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
		if len(subject) > maxSubjectBytes {
			return outboundMessage{}, nil, fmt.Errorf("reply subject exceeds %d bytes", maxSubjectBytes)
		}
	}
	textBody := rawStringValue(input, "text_body")
	if textBody == "" || len(textBody) > maxTextBodyBytes {
		return outboundMessage{}, nil, fmt.Errorf("text_body is required and must not exceed %d bytes", maxTextBodyBytes)
	}
	htmlSource := strings.TrimSpace(rawStringValue(input, "html_body"))
	sanitizedHTML := ""
	htmlNormalized := false
	if htmlSource != "" {
		if len(htmlSource) > maxHTMLBodyBytes {
			return outboundMessage{}, nil, fmt.Errorf("html_body exceeds %d bytes", maxHTMLBodyBytes)
		}
		sanitizedHTML = sanitizeOutboundHTML(htmlSource)
		if sanitizedHTML == "" {
			return outboundMessage{}, nil, fmt.Errorf("html_body contains no supported formatted content")
		}
		if len(sanitizedHTML) > maxHTMLBodyBytes {
			return outboundMessage{}, nil, fmt.Errorf("sanitized html_body exceeds %d bytes", maxHTMLBodyBytes)
		}
		htmlNormalized = sanitizedHTML != htmlSource
	}
	message := outboundMessage{To: to, CC: cc, BCC: bcc, Subject: subject, TextBody: textBody, HTMLBody: sanitizedHTML, HTMLNormalized: htmlNormalized}
	payload := map[string]any{
		"to":        addressStrings(to),
		"cc":        addressStrings(cc),
		"bcc":       addressStrings(bcc),
		"subject":   subject,
		"text_body": textBody,
		"html_body": sanitizedHTML,
	}
	if actionName == ActionReplyMessage {
		if err := requireIMAP(profile); err != nil {
			return outboundMessage{}, nil, err
		}
		ref, err := parseMessageRef(input["message_ref"])
		if err != nil {
			return outboundMessage{}, nil, err
		}
		if _, err := requireFolder(ref.Folder, profile.AllowedReadFolders); err != nil {
			return outboundMessage{}, nil, err
		}
		message.SourceRef = &ref
		payload["message_ref"] = ref.mapValue()
	}
	return message, payload, nil
}

func parseAddressInput(value any, allowedDomains []string) ([]*mail.Address, error) {
	var parsed []*mail.Address
	if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
		addresses, err := mail.ParseAddressList(text)
		if err != nil {
			return nil, fmt.Errorf("invalid recipient address list")
		}
		parsed = addresses
	} else {
		for _, item := range stringSlice(value) {
			address, err := parseMailboxAddress(item)
			if err != nil {
				return nil, err
			}
			parsed = append(parsed, address)
		}
	}
	if len(parsed) == 0 {
		return nil, nil
	}
	result := make([]*mail.Address, 0, len(parsed))
	seen := map[string]bool{}
	for _, address := range parsed {
		normalized, err := parseMailboxAddress(address.String())
		if err != nil {
			return nil, err
		}
		address = normalized
		if !recipientDomainAllowed(address, allowedDomains) {
			return nil, fmt.Errorf("recipient domain is outside the target allowlist")
		}
		key := strings.ToLower(address.Address)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, address)
	}
	return result, nil
}

func addressStrings(addresses []*mail.Address) []string {
	values := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if address != nil {
			values = append(values, address.String())
		}
	}
	return values
}

func dedupeAddressGroups(groups ...[]*mail.Address) ([]*mail.Address, []*mail.Address, []*mail.Address) {
	normalized := make([][]*mail.Address, len(groups))
	seen := map[string]bool{}
	for groupIndex, group := range groups {
		for _, address := range group {
			if address == nil {
				continue
			}
			key := strings.ToLower(address.Address)
			if seen[key] {
				continue
			}
			seen[key] = true
			normalized[groupIndex] = append(normalized[groupIndex], address)
		}
	}
	return normalized[0], normalized[1], normalized[2]
}

func sanitizeOutboundHTML(source string) string {
	return strings.TrimSpace(outboundHTMLPolicy.Sanitize(source))
}

func newOutboundHTMLPolicy() *bluemonday.Policy {
	policy := bluemonday.NewPolicy()
	// Chromium inserts div blocks when Enter is pressed in a contentEditable
	// surface. Preserve those blocks so adjacent lines cannot collapse during
	// sanitization; the plain-text projection already treats div as a boundary.
	policy.AllowElements("p", "div", "br", "strong", "b", "em", "i", "u", "h1", "h2", "h3", "h4", "ol", "ul", "li", "blockquote", "code", "pre", "a")
	policy.AllowAttrs("href").OnElements("a")
	policy.AllowURLSchemes("https", "http", "mailto")
	return policy
}
