package mailconnector

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/textproto"
	"sort"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

const (
	uidSearchWindow = uint32(1000)
	maxUIDsScanned  = uint32(10000)
)

type searchCursor struct {
	Version       int    `json:"v"`
	Order         string `json:"order"`
	Folder        string `json:"folder"`
	UIDValidity   uint32 `json:"uidvalidity"`
	QueryHash     string `json:"query_hash"`
	NextBeforeUID uint32 `json:"next_before_uid"`
}

type normalizedSearch struct {
	Folder     string `json:"folder"`
	UnreadOnly bool   `json:"unread_only"`
	Sender     string `json:"sender,omitempty"`
	Recipient  string `json:"recipient,omitempty"`
	Subject    string `json:"subject,omitempty"`
	Since      string `json:"since,omitempty"`
	Before     string `json:"before,omitempty"`
	sinceTime  time.Time
	beforeTime time.Time
}

type imapMailboxSelector interface {
	Select(name string, readOnly bool) (*imap.MailboxStatus, error)
}

type uidSearchClient interface {
	UidSearch(criteria *imap.SearchCriteria) ([]uint32, error)
}

type mailboxStatusClient interface {
	Status(name string, items []imap.StatusItem) (*imap.MailboxStatus, error)
}

func executeReadAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, readActionTimeout)
	defer cancel()
	target, err := targetConfigFrom(runtime.Target)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	profile, err := profileConfigFrom(runtime.Profile)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if err := requireIMAP(profile); err != nil {
		return connectors.ActionResult{}, err
	}
	secrets, err := loadIMAPSecrets(ctx, runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	imapClient, err := openIMAP(ctx, runtime, target, profile, secrets)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	defer closeIMAP(imapClient)

	var output any
	switch action.ActionName {
	case ActionListFolders:
		output, err = listFolders(ctx, imapClient, profile)
	case ActionCheckMailbox, ActionSearchMessages:
		output, err = searchMessages(ctx, imapClient, profile, action)
	case ActionGetMessage:
		output, err = getMessage(ctx, imapClient, profile, action.Payload, true)
	case ActionListAttachments:
		output, err = getMessage(ctx, imapClient, profile, action.Payload, false)
	default:
		return connectors.ActionResult{}, ErrUnsupportedAction
	}
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if err := validateResultSize(output); err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: output, DisplayText: readDisplayText(action.ActionName, output)}, nil
}

func listFolders(ctx context.Context, imapClient *client.Client, profile profileConfig) (map[string]any, error) {
	setIMAPTimeout(ctx, imapClient)
	mailboxes := make(chan *imap.MailboxInfo)
	errCh := make(chan error, 1)
	go func() { errCh <- imapClient.List("", "*", mailboxes) }()
	folders := make([]map[string]any, 0, len(profile.AllowedReadFolders))
	truncated := false
	for mailbox := range mailboxes {
		if mailbox == nil || !folderAllowed(mailbox.Name, profile.AllowedReadFolders) {
			continue
		}
		if len(folders) >= maxFolderRows {
			truncated = true
			continue
		}
		if err := validateFolderName(mailbox.Name); err != nil {
			truncated = true
			continue
		}
		name := mailbox.Name
		folders = append(folders, map[string]any{
			"name":         name,
			"display_name": name,
			"delimiter":    boundedText(mailbox.Delimiter, maxFolderDelimiterBytes),
			"attributes":   boundedServerStrings(mailbox.Attributes),
			"selectable":   !containsFold(mailbox.Attributes, "\\Noselect"),
			"role":         folderRole(mailbox, profile),
		})
	}
	if err := <-errCh; err != nil {
		return nil, classifyProtocolError("IMAP LIST", err)
	}
	sort.SliceStable(folders, func(i, j int) bool {
		return folderPolicyIndex(fmt.Sprint(folders[i]["name"]), profile.AllowedReadFolders) < folderPolicyIndex(fmt.Sprint(folders[j]["name"]), profile.AllowedReadFolders)
	})
	result := map[string]any{"folders": folders, "count": len(folders), "bounded": true, "truncated": truncated, "has_more": truncated}
	addUntrustedContentMetadata(result)
	return result, nil
}

func folderPolicyIndex(folder string, allowed []string) int {
	for index, candidate := range allowed {
		if folderEqual(candidate, folder) {
			return index
		}
	}
	return len(allowed)
}

func searchMessages(ctx context.Context, imapClient *client.Client, profile profileConfig, action connectors.PreparedAction) (map[string]any, error) {
	search, limit, cursorValue, err := normalizedSearchFrom(action)
	if err != nil {
		return nil, err
	}
	folder, err := requireFolder(search.Folder, profile.AllowedReadFolders)
	if err != nil {
		return nil, err
	}
	setIMAPTimeout(ctx, imapClient)
	status, err := examineMailbox(imapClient, folder)
	if err != nil {
		return nil, err
	}
	if status == nil || status.UidValidity == 0 {
		return nil, fmt.Errorf("IMAP mailbox did not provide UIDVALIDITY")
	}
	unread, err := mailboxUnreadCount(ctx, imapClient, folder)
	if err != nil {
		return nil, err
	}
	queryHash, err := searchFingerprint(search)
	if err != nil {
		return nil, err
	}
	high := status.UidNext
	if high > 0 {
		high--
	}
	if cursorValue != "" {
		cursor, err := decodeSearchCursor(cursorValue)
		if err != nil {
			return nil, err
		}
		if cursor.Folder != folder || cursor.UIDValidity != status.UidValidity || cursor.QueryHash != queryHash {
			return nil, fmt.Errorf("cursor does not belong to this folder, mailbox version, or search")
		}
		if cursor.NextBeforeUID == 0 {
			high = 0
		} else if cursor.NextBeforeUID-1 < high {
			high = cursor.NextBeforeUID - 1
		}
	}
	messages := make([]map[string]any, 0, limit+1)
	var scanned uint32
	resumeBeforeUID := uint32(0)
	exhausted := high == 0
	for high > 0 && scanned < maxUIDsScanned && len(messages) < limit+1 {
		uids, chunkScanned, scanNextBeforeUID, chunkExhausted, searchErr := boundedUIDSearch(ctx, imapClient, search, high, limit+1-len(messages), maxUIDsScanned-scanned)
		if searchErr != nil {
			return nil, searchErr
		}
		scanned += chunkScanned
		exhausted = chunkExhausted
		if len(uids) > 0 {
			rows, fetchErr := fetchEnvelopeRows(ctx, imapClient, folder, status.UidValidity, uids, search)
			if fetchErr != nil {
				return nil, fetchErr
			}
			messages = append(messages, rows...)
		}
		if scanNextBeforeUID > 0 {
			resumeBeforeUID = scanNextBeforeUID
			high = scanNextBeforeUID - 1
		}
		if chunkExhausted || chunkScanned == 0 {
			break
		}
	}
	hasExtraMatch := len(messages) > limit
	hasMore := hasExtraMatch || !exhausted
	if hasExtraMatch {
		if lastVisibleRef, refErr := parseMessageRef(messages[limit-1]["message_ref"]); refErr == nil {
			resumeBeforeUID = lastVisibleRef.UID
		}
		messages = messages[:limit]
	}
	result := map[string]any{
		"folder":             folder,
		"uidvalidity":        status.UidValidity,
		"total":              status.Messages,
		"unread":             unread,
		"messages":           messages,
		"count":              len(messages),
		"has_more":           hasMore,
		"uids_scanned":       scanned,
		"scan_limit_reached": !exhausted && scanned >= maxUIDsScanned,
	}
	addUntrustedContentMetadata(result)
	if hasMore {
		cursor, err := encodeSearchCursor(searchCursor{Version: 1, Order: "uid_desc", Folder: folder, UIDValidity: status.UidValidity, QueryHash: queryHash, NextBeforeUID: resumeBeforeUID})
		if err != nil {
			return nil, err
		}
		result["next_cursor"] = cursor
	}
	return result, nil
}

func boundedUIDSearch(ctx context.Context, imapClient uidSearchClient, search normalizedSearch, high uint32, want int, scanLimit uint32) ([]uint32, uint32, uint32, bool, error) {
	if high == 0 {
		return nil, 0, 0, true, nil
	}
	if scanLimit == 0 || scanLimit > maxUIDsScanned {
		scanLimit = maxUIDsScanned
	}
	result := make([]uint32, 0, want)
	var scanned uint32
	for high > 0 && scanned < scanLimit && len(result) < want {
		if err := ctx.Err(); err != nil {
			return nil, scanned, 0, false, err
		}
		window := uidSearchWindow
		if remaining := scanLimit - scanned; remaining < window {
			window = remaining
		}
		low := uint32(1)
		if high >= window {
			low = high - window + 1
		}
		criteria, err := imapCriteria(search)
		if err != nil {
			return nil, scanned, 0, false, err
		}
		criteria.Uid = new(imap.SeqSet)
		criteria.Uid.AddRange(low, high)
		if liveClient, ok := imapClient.(*client.Client); ok {
			setIMAPTimeout(ctx, liveClient)
		}
		uids, err := imapClient.UidSearch(criteria)
		if err != nil {
			return nil, scanned, 0, false, classifyProtocolError("IMAP UID SEARCH", err)
		}
		sort.Slice(uids, func(i, j int) bool { return uids[i] > uids[j] })
		result = append(result, uids...)
		scanned += high - low + 1
		if low == 1 {
			high = 0
		} else {
			high = low - 1
		}
	}
	nextBeforeUID := uint32(0)
	if high > 0 {
		nextBeforeUID = high + 1
	}
	return result, scanned, nextBeforeUID, high == 0, nil
}

func fetchEnvelopeRows(ctx context.Context, imapClient *client.Client, folder string, uidValidity uint32, uids []uint32, search normalizedSearch) ([]map[string]any, error) {
	if len(uids) == 0 {
		return []map[string]any{}, nil
	}
	set := new(imap.SeqSet)
	set.AddNum(uids...)
	messages := make(chan *imap.Message)
	errCh := make(chan error, 1)
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchFlags, imap.FetchInternalDate, imap.FetchRFC822Size}
	setIMAPTimeout(ctx, imapClient)
	go func() { errCh <- imapClient.UidFetch(set, items, messages) }()
	byUID := map[uint32]map[string]any{}
	for message := range messages {
		if message == nil || message.Uid == 0 || message.Envelope == nil {
			continue
		}
		if !search.sinceTime.IsZero() {
			if message.InternalDate.Before(search.sinceTime) {
				continue
			}
		}
		if !search.beforeTime.IsZero() {
			if !message.InternalDate.Before(search.beforeTime) {
				continue
			}
		}
		byUID[message.Uid] = envelopeRow(folder, uidValidity, message)
	}
	if err := <-errCh; err != nil {
		return nil, classifyProtocolError("IMAP UID FETCH", err)
	}
	rows := make([]map[string]any, 0, len(byUID))
	for _, uid := range uids {
		if row := byUID[uid]; row != nil {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func getMessage(ctx context.Context, imapClient *client.Client, profile profileConfig, payload map[string]any, includeBody bool) (map[string]any, error) {
	ref, err := parseMessageRef(payload["message_ref"])
	if err != nil {
		return nil, err
	}
	if _, err := requireFolder(ref.Folder, profile.AllowedReadFolders); err != nil {
		return nil, err
	}
	setIMAPTimeout(ctx, imapClient)
	status, err := examineMailbox(imapClient, ref.Folder)
	if err != nil {
		return nil, err
	}
	if err := requireUIDValidity(status, ref.UIDValidity, "message reference"); err != nil {
		return nil, err
	}
	message, err := fetchOneMessage(ctx, imapClient, ref.UID, []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchFlags, imap.FetchInternalDate, imap.FetchRFC822Size, imap.FetchBodyStructure}, nil)
	if err != nil {
		return nil, err
	}
	if message == nil {
		return nil, connectors.ClassifyError("stale_message_reference", fmt.Errorf("message reference is stale because the UID no longer exists"))
	}
	if message.Envelope == nil || message.BodyStructure == nil {
		return nil, fmt.Errorf("message was not found or has incomplete envelope/body metadata")
	}
	attachments, encrypted, signed, attachmentsTruncated := attachmentRows(message.BodyStructure)
	result := envelopeRow(ref.Folder, ref.UIDValidity, message)
	result["attachments"] = attachments
	result["attachment_count"] = len(attachments)
	result["attachments_truncated"] = attachmentsTruncated
	result["encrypted_content"] = encrypted
	result["signed_content"] = signed
	addUntrustedContentMetadata(result)
	if !includeBody {
		return result, nil
	}
	parts := preferredTextParts(message.BodyStructure)
	if len(parts) == 0 {
		result["body"] = ""
		result["body_available"] = false
		result["body_truncated"] = false
		return result, nil
	}
	var selected bodyPart
	var body string
	var decodedBytes int
	var truncated bool
	var decodedSizeComplete bool
	for index, part := range parts {
		section := textBodySection(part)
		bodyMessage, err := fetchOneMessage(ctx, imapClient, ref.UID, []imap.FetchItem{imap.FetchUid, section.FetchItem()}, section)
		if err != nil {
			return nil, err
		}
		if bodyMessage == nil {
			return nil, fmt.Errorf("message body was not found")
		}
		literal := bodyMessage.GetBody(section)
		if literal == nil {
			return nil, fmt.Errorf("message body section was not returned")
		}
		body, decodedBytes, truncated, decodedSizeComplete, err = decodeTextPart(literal, part.Structure, maxBodyBytes)
		if err != nil {
			return nil, err
		}
		selected = part
		if strings.TrimSpace(body) != "" || index == len(parts)-1 {
			break
		}
	}
	result["body"] = body
	result["body_available"] = true
	sourceContentType := strings.ToLower(selected.Structure.MIMEType + "/" + selected.Structure.MIMESubType)
	setBodyProjectionMetadata(result, sourceContentType)
	result["body_truncated"] = truncated || selected.Structure.Size > maxWireBodyBytes
	result["body_declared_bytes"] = selected.Structure.Size
	result["body_decoded_bytes_observed"] = decodedBytes
	decodedSizeComplete = decodedSizeComplete && selected.Structure.Size <= maxWireBodyBytes
	result["body_decoded_size_complete"] = decodedSizeComplete
	if decodedSizeComplete {
		result["body_decoded_bytes"] = decodedBytes
	}
	result["body_returned_bytes"] = len(body)
	return result, nil
}

func setBodyProjectionMetadata(result map[string]any, sourceContentType string) {
	result["body_content_type"] = sourceContentType
	if sourceContentType == "text/html" {
		result["body_content_type"] = "text/plain"
		result["body_source_content_type"] = sourceContentType
		result["body_projection"] = "html_to_text"
	}
}

func textBodySection(part bodyPart) *imap.BodySectionName {
	name := imap.BodyPartName{Path: part.Path}
	if len(part.Path) == 0 {
		name.Specifier = imap.TextSpecifier
	}
	return &imap.BodySectionName{BodyPartName: name, Peek: true, Partial: []int{0, maxWireBodyBytes}}
}

func fetchOneMessage(ctx context.Context, imapClient *client.Client, uid uint32, items []imap.FetchItem, section *imap.BodySectionName) (*imap.Message, error) {
	set := new(imap.SeqSet)
	set.AddNum(uid)
	messages := make(chan *imap.Message)
	errCh := make(chan error, 1)
	setIMAPTimeout(ctx, imapClient)
	go func() { errCh <- imapClient.UidFetch(set, items, messages) }()
	var found *imap.Message
	for message := range messages {
		if message != nil && message.Uid == uid {
			found = message
		}
	}
	if err := <-errCh; err != nil {
		return nil, classifyProtocolError("IMAP UID FETCH", err)
	}
	return found, nil
}

func normalizedSearchFrom(action connectors.PreparedAction) (normalizedSearch, int, string, error) {
	payload := action.Payload
	if err := normalizeAndValidateSearchPayload(payload); err != nil {
		return normalizedSearch{}, 0, "", err
	}
	search := normalizedSearch{
		Folder:     stringValue(payload, "folder"),
		UnreadOnly: boolValue(payload, "unread_only", action.ActionName == ActionCheckMailbox),
		Sender:     stringValue(payload, "sender"),
		Recipient:  stringValue(payload, "recipient"),
		Subject:    stringValue(payload, "subject"),
		Since:      stringValue(payload, "since"),
		Before:     stringValue(payload, "before"),
	}
	for label, value := range map[string]string{"since": search.Since, "before": search.Before} {
		if value == "" {
			continue
		}
		parsed, _ := time.Parse(time.RFC3339, value)
		if label == "since" {
			search.sinceTime = parsed
		} else {
			search.beforeTime = parsed
		}
	}
	fallback := defaultMessageRows
	if action.ActionName == ActionCheckMailbox {
		fallback = defaultMailboxRows
	}
	limit, err := boundedExactInt(payload, "limit", fallback, 1, maxMessageRows)
	if err != nil {
		return normalizedSearch{}, 0, "", fmt.Errorf("limit %w", err)
	}
	return search, limit, stringValue(payload, "cursor"), nil
}

func imapCriteria(search normalizedSearch) (*imap.SearchCriteria, error) {
	criteria := imap.NewSearchCriteria()
	criteria.Header = textproto.MIMEHeader{}
	if search.UnreadOnly {
		criteria.WithoutFlags = append(criteria.WithoutFlags, imap.SeenFlag)
	}
	if search.Sender != "" {
		criteria.Header.Set("From", search.Sender)
	}
	if search.Recipient != "" {
		to := imap.NewSearchCriteria()
		to.Header.Set("To", search.Recipient)
		cc := imap.NewSearchCriteria()
		cc.Header.Set("Cc", search.Recipient)
		criteria.Or = append(criteria.Or, [2]*imap.SearchCriteria{to, cc})
	}
	if search.Subject != "" {
		criteria.Header.Set("Subject", search.Subject)
	}
	if !search.sinceTime.IsZero() {
		criteria.Since = search.sinceTime
	} else if search.Since != "" {
		parsed, err := time.Parse(time.RFC3339, search.Since)
		if err != nil {
			return nil, fmt.Errorf("since must be an RFC3339 timestamp")
		}
		criteria.Since = parsed
	}
	if !search.beforeTime.IsZero() {
		criteria.Before = search.beforeTime.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	} else if search.Before != "" {
		parsed, err := time.Parse(time.RFC3339, search.Before)
		if err != nil {
			return nil, fmt.Errorf("before must be an RFC3339 timestamp")
		}
		criteria.Before = parsed.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	}
	return criteria, nil
}

func envelopeRow(folder string, uidValidity uint32, message *imap.Message) map[string]any {
	envelope := message.Envelope
	return map[string]any{
		"message_ref": messageRef{Folder: folder, UIDValidity: uidValidity, UID: message.Uid}.mapValue(),
		"uid":         message.Uid,
		"subject":     boundedText(envelope.Subject, maxSubjectBytes),
		"message_id":  boundedText(envelope.MessageId, 1000),
		"from":        addressRows(envelope.From),
		"to":          addressRows(envelope.To),
		"cc":          addressRows(envelope.Cc),
		"reply_to":    addressRows(envelope.ReplyTo),
		"header_date": formatTime(envelope.Date),
		"received_at": formatTime(message.InternalDate),
		"flags":       boundedServerStrings(message.Flags),
		"read":        containsFold(message.Flags, imap.SeenFlag),
		"size_bytes":  message.Size,
	}
}

func addressRows(addresses []*imap.Address) []map[string]any {
	if len(addresses) > maxRecipients {
		addresses = addresses[:maxRecipients]
	}
	rows := make([]map[string]any, 0, len(addresses))
	for _, address := range addresses {
		if address == nil {
			continue
		}
		rows = append(rows, map[string]any{"name": boundedText(address.PersonalName, 512), "address": boundedText(address.Address(), maxAddressBytes)})
	}
	return rows
}

func searchFingerprint(search normalizedSearch) (string, error) {
	data, err := json.Marshal(search)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func encodeSearchCursor(cursor searchCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeSearchCursor(value string) (searchCursor, error) {
	if len(value) == 0 || len(value) > maxCursorBytes {
		return searchCursor{}, fmt.Errorf("cursor is invalid")
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(data) > 4096 {
		return searchCursor{}, fmt.Errorf("cursor is invalid")
	}
	var cursor searchCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.Version != 1 || cursor.Order != "uid_desc" || cursor.Folder == "" || cursor.UIDValidity == 0 || cursor.QueryHash == "" {
		return searchCursor{}, fmt.Errorf("cursor is invalid")
	}
	return cursor, nil
}

func setIMAPTimeout(ctx context.Context, imapClient *client.Client) {
	timeout := commandTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	imapClient.Timeout = timeout
}

func validateResultSize(output any) error {
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("mail result cannot be serialized: %w", err)
	}
	if len(data) > maxResultBytes {
		return fmt.Errorf("mail result exceeds %d bytes", maxResultBytes)
	}
	return nil
}

func readDisplayText(action string, _ any) string {
	return fmt.Sprintf("%s completed.", action)
}

func requireUIDValidity(status *imap.MailboxStatus, expected uint32, subject string) error {
	if status != nil && status.UidValidity == expected {
		return nil
	}
	return connectors.ClassifyError("stale_message_reference", fmt.Errorf("%s is stale because UIDVALIDITY changed", subject))
}

func addUntrustedContentMetadata(output map[string]any) {
	output["trust"] = "untrusted_external_content"
	output["warning"] = untrustedContentWarning
}

func examineMailbox(client imapMailboxSelector, folder string) (*imap.MailboxStatus, error) {
	status, err := client.Select(folder, true)
	if err != nil {
		return nil, classifyProtocolError("IMAP EXAMINE", err)
	}
	return status, nil
}

func containsFold(items []string, value string) bool {
	for _, item := range items {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}

func specialUseRole(mailbox *imap.MailboxInfo) string {
	for _, attribute := range mailbox.Attributes {
		switch strings.ToLower(attribute) {
		case "\\inbox":
			return "inbox"
		case "\\sent":
			return "sent"
		case "\\archive", "\\all":
			return "archive"
		case "\\trash":
			return "trash"
		case "\\junk":
			return "junk"
		case "\\drafts":
			return "drafts"
		}
	}
	if strings.EqualFold(mailbox.Name, defaultFolder) {
		return "inbox"
	}
	return ""
}

func folderRole(mailbox *imap.MailboxInfo, profile profileConfig) string {
	role := specialUseRole(mailbox)
	if role == "" && mailbox != nil && profile.SentFolder != "" && folderEqual(mailbox.Name, profile.SentFolder) {
		return "sent"
	}
	if role == "" && mailbox != nil && profile.ArchiveFolder != "" && folderEqual(mailbox.Name, profile.ArchiveFolder) {
		return "archive"
	}
	if role == "" && mailbox != nil && profile.TrashFolder != "" && folderEqual(mailbox.Name, profile.TrashFolder) {
		return "trash"
	}
	return role
}

func mailboxUnreadCount(ctx context.Context, imapClient mailboxStatusClient, folder string) (uint32, error) {
	if liveClient, ok := imapClient.(*client.Client); ok {
		setIMAPTimeout(ctx, liveClient)
	}
	status, err := imapClient.Status(folder, []imap.StatusItem{imap.StatusUnseen})
	if err != nil {
		return 0, classifyProtocolError("IMAP STATUS", err)
	}
	if status == nil {
		return 0, fmt.Errorf("IMAP STATUS did not return mailbox metadata")
	}
	return status.Unseen, nil
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func boundedText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' {
			return ' '
		}
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.ToValidUTF8(value, "�"))
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "")
}

func boundedServerStrings(values []string) []string {
	if len(values) > maxServerMetadataRows {
		values = values[:maxServerMetadataRows]
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, boundedText(value, maxServerMetadataBytes))
	}
	return result
}
