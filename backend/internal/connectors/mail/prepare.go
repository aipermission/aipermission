package mailconnector

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type messageRef struct {
	Folder      string `json:"folder"`
	UIDValidity uint32 `json:"uidvalidity"`
	UID         uint32 `json:"uid"`
}

func (ref messageRef) mapValue() map[string]any {
	return map[string]any{"folder": ref.Folder, "uidvalidity": ref.UIDValidity, "uid": ref.UID}
}

func (Connector) PrepareAction(_ context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	targetConfig, err := targetConfigFrom(req.Target)
	if err != nil {
		return connectors.PreparedAction{}, err
	}
	profile, err := profileConfigFrom(req.Profile)
	if err != nil {
		return connectors.PreparedAction{}, err
	}
	payload := copyMap(req.Input)
	preview := map[string]any{}
	title := ""
	summary := ""
	risk := connectors.RiskRead

	switch req.ActionName {
	case ActionListFolders:
		if err := requireIMAP(profile); err != nil {
			return connectors.PreparedAction{}, err
		}
		title = "List mail folders"
		summary = "List folders allowed by the selected mailbox profile."
	case ActionCheckMailbox, ActionSearchMessages:
		if err := requireIMAP(profile); err != nil {
			return connectors.PreparedAction{}, err
		}
		folder, err := requireFolder(stringValue(payload, "folder"), profile.AllowedReadFolders)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		payload["folder"] = folder
		fallbackLimit := defaultMessageRows
		if req.ActionName == ActionCheckMailbox {
			fallbackLimit = defaultMailboxRows
		}
		limit, err := boundedExactInt(payload, "limit", fallbackLimit, 1, maxMessageRows)
		if err != nil {
			return connectors.PreparedAction{}, fmt.Errorf("limit %w", err)
		}
		payload["limit"] = limit
		if err := normalizeAndValidateSearchPayload(payload); err != nil {
			return connectors.PreparedAction{}, err
		}
		title = "Check mailbox"
		summary = fmt.Sprintf("Read up to %d message envelope(s) from %s without changing Seen state.", limit, folder)
		if req.ActionName == ActionSearchMessages {
			title = "Search mail messages"
			summary = fmt.Sprintf("Search up to %d message envelope(s) in %s without changing Seen state.", limit, folder)
		}
		preview = copyMap(payload)
	case ActionGetMessage, ActionListAttachments:
		if err := requireIMAP(profile); err != nil {
			return connectors.PreparedAction{}, err
		}
		ref, err := parseMessageRef(payload["message_ref"])
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		if _, err := requireFolder(ref.Folder, profile.AllowedReadFolders); err != nil {
			return connectors.PreparedAction{}, err
		}
		payload["message_ref"] = ref.mapValue()
		title = "Read mail message"
		summary = fmt.Sprintf("Read bounded message content from %s at UID %d without changing Seen state.", ref.Folder, ref.UID)
		if req.ActionName == ActionListAttachments {
			title = "List mail attachments"
			summary = fmt.Sprintf("List attachment metadata for %s UID %d without downloading content.", ref.Folder, ref.UID)
		}
		preview = map[string]any{"message_ref": ref.mapValue()}
	case ActionMarkRead, ActionMarkUnread:
		risk = connectors.RiskWrite
		ref, err := prepareMutationRef(profile, payload)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		title = "Mark mail message read"
		if req.ActionName == ActionMarkUnread {
			title = "Mark mail message unread"
		}
		summary = fmt.Sprintf("Change only the Seen flag for %s UID %d.", ref.Folder, ref.UID)
		preview = map[string]any{"message_ref": ref.mapValue(), "seen": req.ActionName == ActionMarkRead}
	case ActionMoveMessage, ActionArchiveMessage:
		risk = connectors.RiskWrite
		ref, err := prepareMutationRef(profile, payload)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		destination := stringValue(payload, "destination_folder")
		if req.ActionName == ActionArchiveMessage && destination == "" {
			destination = profile.ArchiveFolder
		}
		if destination == "" {
			return connectors.PreparedAction{}, fmt.Errorf("archive or move destination is not configured")
		}
		destination, err = requireExplicitFolder(destination, profile.AllowedMutationDestinations)
		if err != nil {
			return connectors.PreparedAction{}, fmt.Errorf("archive or move destination: %w", err)
		}
		if folderEqual(ref.Folder, destination) {
			return connectors.PreparedAction{}, fmt.Errorf("source and destination folders must differ")
		}
		payload["destination_folder"] = destination
		title = "Move mail message"
		if req.ActionName == ActionArchiveMessage {
			title = "Archive mail message"
		}
		summary = fmt.Sprintf("Move %s UID %d to %s.", ref.Folder, ref.UID, destination)
		preview = map[string]any{"message_ref": ref.mapValue(), "destination_folder": destination}
	case ActionDeleteMessage:
		risk = connectors.RiskDestructive
		ref, err := prepareMutationRef(profile, payload)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		if profile.TrashFolder == "" {
			return connectors.PreparedAction{}, fmt.Errorf("trash_folder is not configured for this mailbox profile")
		}
		trash, err := requireExplicitFolder(profile.TrashFolder, profile.AllowedMutationDestinations)
		if err != nil {
			return connectors.PreparedAction{}, err
		}
		if folderEqual(ref.Folder, trash) {
			return connectors.PreparedAction{}, connectors.ClassifyError("permanent_delete_unsupported", fmt.Errorf("delete_message refuses messages already in the configured Trash folder"))
		}
		payload["destination_folder"] = trash
		title = "Move mail message to Trash"
		summary = fmt.Sprintf("Move %s UID %d to %s without permanent expunge.", ref.Folder, ref.UID, trash)
		preview = map[string]any{"message_ref": ref.mapValue(), "destination_folder": trash, "permanent_expunge": false}
	case ActionSendMessage, ActionReplyMessage:
		return prepareOutboundAction(req, targetConfig, profile)
	default:
		return connectors.PreparedAction{}, ErrUnsupportedAction
	}

	return connectors.PreparedAction{
		ConnectorKind:   Kind,
		TargetRef:       req.Target.Ref,
		ProfileID:       req.Profile.ID,
		ActionName:      req.ActionName,
		Risk:            risk,
		Title:           title,
		Summary:         summary,
		Preview:         preview,
		Payload:         payload,
		ContextMaterial: contextMaterial(targetConfig, profile),
	}, nil
}

func normalizeAndValidateSearchPayload(payload map[string]any) error {
	for _, field := range []string{"since", "before"} {
		if value := stringValue(payload, field); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return fmt.Errorf("%s must be an RFC3339 timestamp", field)
			}
			payload[field] = parsed.UTC().Format(time.RFC3339)
		}
	}
	for field, maximum := range map[string]int{"sender": maxAddressBytes, "recipient": maxAddressBytes, "subject": maxSearchTextBytes} {
		if value := stringValue(payload, field); len(value) > maximum {
			return fmt.Errorf("%s search exceeds %d bytes", field, maximum)
		}
	}
	if cursor := stringValue(payload, "cursor"); len(cursor) > maxCursorBytes {
		return fmt.Errorf("cursor exceeds %d bytes", maxCursorBytes)
	}
	return nil
}

func (Connector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	switch action.ActionName {
	case ActionListFolders, ActionCheckMailbox, ActionSearchMessages, ActionGetMessage, ActionListAttachments:
		return executeReadAction(ctx, runtime, action)
	case ActionMarkRead, ActionMarkUnread, ActionMoveMessage, ActionArchiveMessage, ActionDeleteMessage:
		return executeMutationAction(ctx, runtime, action)
	case ActionSendMessage, ActionReplyMessage:
		return executeOutboundAction(ctx, runtime, action)
	default:
		return connectors.ActionResult{}, ErrUnsupportedAction
	}
}

func contextMaterial(target targetConfig, profile profileConfig) map[string]any {
	return map[string]any{
		"connection_mode":                      target.ConnectionMode,
		"transport_target_ref":                 target.TransportTargetRef,
		"imap_endpoint":                        endpointURL(target.IMAPHost, target.IMAPPort),
		"imap_tls_mode":                        target.IMAPTLSMode,
		"smtp_endpoint":                        endpointURL(target.SMTPHost, target.SMTPPort),
		"smtp_tls_mode":                        target.SMTPTLSMode,
		"allowed_recipient_domains":            append([]string(nil), target.AllowedRecipientDomains...),
		"mailbox_address":                      profile.MailboxAddress,
		"imap_enabled":                         profile.IMAPEnabled,
		"smtp_auth_mode":                       profile.SMTPAuthMode,
		"allowed_read_folders":                 append([]string(nil), profile.AllowedReadFolders...),
		"allowed_mutation_source_folders":      append([]string(nil), profile.AllowedMutationSources...),
		"allowed_mutation_destination_folders": append([]string(nil), profile.AllowedMutationDestinations...),
		"sent_folder":                          profile.SentFolder,
		"archive_folder":                       profile.ArchiveFolder,
		"trash_folder":                         profile.TrashFolder,
	}
}

func prepareMutationRef(profile profileConfig, payload map[string]any) (messageRef, error) {
	if err := requireIMAP(profile); err != nil {
		return messageRef{}, err
	}
	ref, err := parseMessageRef(payload["message_ref"])
	if err != nil {
		return messageRef{}, err
	}
	if _, err := requireFolder(ref.Folder, profile.AllowedMutationSources); err != nil {
		return messageRef{}, err
	}
	payload["message_ref"] = ref.mapValue()
	return ref, nil
}

func parseMessageRef(value any) (messageRef, error) {
	var raw map[string]any
	switch typed := value.(type) {
	case map[string]any:
		raw = typed
	case string:
		if err := json.Unmarshal([]byte(typed), &raw); err != nil {
			return messageRef{}, fmt.Errorf("message_ref must be a structured object")
		}
	default:
		return messageRef{}, fmt.Errorf("message_ref must be a structured object")
	}
	ref := messageRef{
		Folder:      strings.TrimSpace(fmt.Sprint(raw["folder"])),
		UIDValidity: uint32Value(raw["uidvalidity"]),
		UID:         uint32Value(raw["uid"]),
	}
	if ref.Folder == "" || ref.UIDValidity == 0 || ref.UID == 0 {
		return messageRef{}, fmt.Errorf("message_ref requires folder, uidvalidity, and uid")
	}
	return ref, nil
}

func uint32Value(value any) uint32 {
	parsed, err := strconv.ParseUint(strings.TrimSpace(fmt.Sprint(value)), 10, 32)
	if err != nil {
		if number, ok := value.(float64); ok && number > 0 && number <= float64(^uint32(0)) && math.Trunc(number) == number {
			return uint32(number)
		}
		return 0
	}
	return uint32(parsed)
}

func boundedExactInt(values map[string]any, key string, fallback, minimum, maximum int) (int, error) {
	value, err := exactIntValue(values, key, fallback)
	if err != nil {
		return 0, err
	}
	if value < minimum {
		return minimum, nil
	}
	if value > maximum {
		return maximum, nil
	}
	return value, nil
}

func requireIMAP(profile profileConfig) error {
	if !profile.IMAPEnabled {
		return fmt.Errorf("%w: IMAP is disabled for this profile", ErrInvalidConfig)
	}
	return nil
}

func copyMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
