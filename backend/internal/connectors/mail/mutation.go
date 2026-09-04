package mailconnector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

type imapMutationClient interface {
	Select(name string, readOnly bool) (*imap.MailboxStatus, error)
	Status(name string, items []imap.StatusItem) (*imap.MailboxStatus, error)
	UidSearch(criteria *imap.SearchCriteria) ([]uint32, error)
	UidStore(seqset *imap.SeqSet, item imap.StoreItem, value interface{}, ch chan *imap.Message) error
	Support(cap string) (bool, error)
	UidMove(seqset *imap.SeqSet, dest string) error
}

func executeMutationAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
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
	ref, err := parseMessageRef(action.Payload["message_ref"])
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if _, err := requireFolder(ref.Folder, profile.AllowedMutationSources); err != nil {
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
	setIMAPTimeout(ctx, imapClient)
	output, err := mutateMessage(imapClient, profile, action, ref)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if err := validateResultSize(output); err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: output, DisplayText: readDisplayText(action.ActionName, output)}, nil
}

func mutateMessage(imapClient imapMutationClient, profile profileConfig, action connectors.PreparedAction, ref messageRef) (map[string]any, error) {
	status, err := imapClient.Select(ref.Folder, false)
	if err != nil {
		return nil, classifyProtocolError("IMAP SELECT", err)
	}
	if err := requireUIDValidity(status, ref.UIDValidity, "message reference"); err != nil {
		return nil, err
	}
	set := new(imap.SeqSet)
	set.AddNum(ref.UID)
	criteria := imap.NewSearchCriteria()
	criteria.Uid = set
	uids, err := imapClient.UidSearch(criteria)
	if err != nil {
		return nil, classifyProtocolError("IMAP UID SEARCH", err)
	}
	if len(uids) != 1 || uids[0] != ref.UID {
		return nil, connectors.ClassifyError("stale_message_reference", fmt.Errorf("message reference is stale because UID %d no longer exists", ref.UID))
	}
	switch action.ActionName {
	case ActionMarkRead, ActionMarkUnread:
		var op imap.FlagsOp = imap.AddFlags
		seen := true
		if action.ActionName == ActionMarkUnread {
			op = imap.RemoveFlags
			seen = false
		}
		if err := imapClient.UidStore(set, imap.FormatFlagsOp(op, true), []interface{}{imap.SeenFlag}, nil); err != nil {
			return nil, classifyIMAPMutationError("UID STORE", err)
		}
		return map[string]any{"message_ref": ref.mapValue(), "read": seen, "changed_flag": imap.SeenFlag}, nil
	case ActionMoveMessage, ActionArchiveMessage, ActionDeleteMessage:
		destination := stringValue(action.Payload, "destination_folder")
		if action.ActionName == ActionArchiveMessage && destination == "" {
			destination = profile.ArchiveFolder
		}
		destination, err = requireExplicitFolder(destination, profile.AllowedMutationDestinations)
		if err != nil {
			return nil, err
		}
		if action.ActionName == ActionDeleteMessage {
			if profile.TrashFolder == "" || !folderEqual(destination, profile.TrashFolder) {
				return nil, fmt.Errorf("delete_message destination must be the configured Trash folder")
			}
			if folderEqual(ref.Folder, profile.TrashFolder) {
				return nil, connectors.ClassifyError("permanent_delete_unsupported", fmt.Errorf("delete_message refuses messages already in Trash"))
			}
		}
		if folderEqual(ref.Folder, destination) {
			return nil, fmt.Errorf("source and destination folders must differ")
		}
		if _, err := imapClient.Status(destination, []imap.StatusItem{imap.StatusMessages}); err != nil {
			return nil, connectors.ClassifyError("folder_unavailable", fmt.Errorf("destination folder is unavailable or not selectable"))
		}
		supported, err := imapClient.Support("MOVE")
		if err != nil {
			return nil, classifyProtocolError("IMAP MOVE capability", err)
		}
		if !supported {
			return nil, fmt.Errorf("IMAP server does not support UID-safe MOVE; unsafe copy/delete/expunge fallback was not used")
		}
		if err := imapClient.UidMove(set, destination); err != nil {
			return nil, classifyIMAPMutationError("UID MOVE", err)
		}
		return map[string]any{
			"source_ref":                ref.mapValue(),
			"destination_folder":        destination,
			"destination_ref_available": false,
			"permanent_expunge":         false,
		}, nil
	default:
		return nil, ErrUnsupportedAction
	}
}

func classifyIMAPMutationError(operation string, err error) error {
	if !isAmbiguousIMAPTransportError(err) {
		return classifyProtocolError("IMAP "+operation, err)
	}
	return connectors.ClassifyActionError(
		"outcome_unknown",
		connectors.ResultOutcomeUnknown,
		map[string]any{"dispatch_stage": "imap_command", "retry_safe": false},
		fmt.Errorf("IMAP %s outcome is unknown after dispatch; inspect message state before retrying: %w", operation, err),
	)
}

func isAmbiguousIMAPTransportError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{"connection closed", "broken pipe", "connection reset", "unexpected eof"} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

var _ imapMutationClient = (*client.Client)(nil)
