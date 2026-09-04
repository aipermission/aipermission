package mailconnector

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/emersion/go-imap"
)

type fakeMutationClient struct {
	status         *imap.MailboxStatus
	selected       string
	readOnly       bool
	storeItem      imap.StoreItem
	storeValue     interface{}
	moveSupport    bool
	movedTo        string
	storeErr       error
	moveErr        error
	destinationErr error
	missing        bool
}

func (client *fakeMutationClient) Select(name string, readOnly bool) (*imap.MailboxStatus, error) {
	client.selected = name
	client.readOnly = readOnly
	return client.status, nil
}

func (client *fakeMutationClient) Status(string, []imap.StatusItem) (*imap.MailboxStatus, error) {
	if client.destinationErr != nil {
		return nil, client.destinationErr
	}
	return &imap.MailboxStatus{}, nil
}

func (client *fakeMutationClient) UidStore(_ *imap.SeqSet, item imap.StoreItem, value interface{}, _ chan *imap.Message) error {
	client.storeItem = item
	client.storeValue = value
	return client.storeErr
}

func (client *fakeMutationClient) UidSearch(_ *imap.SearchCriteria) ([]uint32, error) {
	if client.missing {
		return nil, nil
	}
	return []uint32{42}, nil
}

func (client *fakeMutationClient) Support(cap string) (bool, error) {
	return cap == "MOVE" && client.moveSupport, nil
}

func (client *fakeMutationClient) UidMove(_ *imap.SeqSet, destination string) error {
	client.movedTo = destination
	return client.moveErr
}

func TestMutateMessageMarksReadAndUnreadWithSilentFlagDelta(t *testing.T) {
	profile, err := profileConfigFrom(connectors.CredentialProfileView{Public: validProfilePublic()})
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	ref := messageRef{Folder: "INBOX", UIDValidity: 7, UID: 42}
	for _, test := range []struct {
		action   string
		wantOp   string
		wantRead bool
	}{{ActionMarkRead, "+FLAGS.SILENT", true}, {ActionMarkUnread, "-FLAGS.SILENT", false}} {
		client := &fakeMutationClient{status: &imap.MailboxStatus{UidValidity: 7}}
		output, err := mutateMessage(client, profile, connectors.PreparedAction{ActionName: test.action}, ref)
		if err != nil {
			t.Fatalf("%s: %v", test.action, err)
		}
		if client.readOnly || string(client.storeItem) != test.wantOp || output["read"] != test.wantRead {
			t.Fatalf("%s item=%q readOnly=%v output=%#v", test.action, client.storeItem, client.readOnly, output)
		}
	}
}

func TestMutateMessageFailsClosedWithoutMoveCapability(t *testing.T) {
	profile, err := profileConfigFrom(connectors.CredentialProfileView{Public: validProfilePublic()})
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	client := &fakeMutationClient{status: &imap.MailboxStatus{UidValidity: 7}}
	_, err = mutateMessage(client, profile, connectors.PreparedAction{
		ActionName: ActionArchiveMessage,
		Payload:    map[string]any{"destination_folder": "Archive"},
	}, messageRef{Folder: "INBOX", UIDValidity: 7, UID: 42})
	if err == nil || !strings.Contains(err.Error(), "unsafe copy/delete/expunge fallback was not used") {
		t.Fatalf("expected safe MOVE failure, got %v", err)
	}
	if client.movedTo != "" {
		t.Fatalf("unexpected move to %q", client.movedTo)
	}
}

func TestMutateMessageRejectsStaleReferenceAndDeleteInsideTrash(t *testing.T) {
	profile, err := profileConfigFrom(connectors.CredentialProfileView{Public: validProfilePublic()})
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	client := &fakeMutationClient{status: &imap.MailboxStatus{UidValidity: 8}, moveSupport: true}
	_, err = mutateMessage(client, profile, connectors.PreparedAction{ActionName: ActionMarkRead}, messageRef{Folder: "INBOX", UIDValidity: 7, UID: 42})
	if err == nil || !strings.Contains(err.Error(), "UIDVALIDITY changed") || connectors.ErrorCode(err) != "stale_message_reference" {
		t.Fatalf("expected stale reference, got %v", err)
	}

	client.status.UidValidity = 7
	_, err = mutateMessage(client, profile, connectors.PreparedAction{ActionName: ActionDeleteMessage, Payload: map[string]any{"destination_folder": "Trash"}}, messageRef{Folder: "Trash", UIDValidity: 7, UID: 42})
	if err == nil || !strings.Contains(err.Error(), "already in Trash") {
		t.Fatalf("expected delete-in-Trash rejection, got %v", err)
	}
	if connectors.ErrorCode(err) != "permanent_delete_unsupported" {
		t.Fatalf("delete-in-Trash code = %q", connectors.ErrorCode(err))
	}
}

func TestMutateMessageDoesNotHideMoveFailure(t *testing.T) {
	profile, err := profileConfigFrom(connectors.CredentialProfileView{Public: validProfilePublic()})
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	client := &fakeMutationClient{status: &imap.MailboxStatus{UidValidity: 7}, moveSupport: true, moveErr: errors.New("server refused")}
	_, err = mutateMessage(client, profile, connectors.PreparedAction{ActionName: ActionMoveMessage, Payload: map[string]any{"destination_folder": "Archive"}}, messageRef{Folder: "INBOX", UIDValidity: 7, UID: 42})
	if err == nil || client.movedTo != "Archive" {
		t.Fatalf("expected classified move failure, moved=%q err=%v", client.movedTo, err)
	}
}

func TestIMAPMutationTransportFailuresAreOutcomeUnknown(t *testing.T) {
	profile, err := profileConfigFrom(connectors.CredentialProfileView{Public: validProfilePublic()})
	if err != nil {
		t.Fatal(err)
	}
	ref := messageRef{Folder: "INBOX", UIDValidity: 7, UID: 42}
	for _, test := range []struct {
		name   string
		action connectors.PreparedAction
		client *fakeMutationClient
	}{
		{name: "store", action: connectors.PreparedAction{ActionName: ActionMarkRead}, client: &fakeMutationClient{status: &imap.MailboxStatus{UidValidity: 7}, storeErr: io.ErrUnexpectedEOF}},
		{name: "move", action: connectors.PreparedAction{ActionName: ActionMoveMessage, Payload: map[string]any{"destination_folder": "Archive"}}, client: &fakeMutationClient{status: &imap.MailboxStatus{UidValidity: 7}, moveSupport: true, moveErr: errors.New("imap: connection closed during command execution")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := mutateMessage(test.client, profile, test.action, ref)
			if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || connectors.ErrorCode(err) != "outcome_unknown" {
				t.Fatalf("transport failure = %v, want outcome_unknown", err)
			}
		})
	}
}

func TestMutateMessageRequiresAnAvailableExplicitDestination(t *testing.T) {
	profile, err := profileConfigFrom(connectors.CredentialProfileView{Public: validProfilePublic()})
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	ref := messageRef{Folder: "INBOX", UIDValidity: 7, UID: 42}
	client := &fakeMutationClient{status: &imap.MailboxStatus{UidValidity: 7}, moveSupport: true}
	_, err = mutateMessage(client, profile, connectors.PreparedAction{ActionName: ActionMoveMessage, Payload: map[string]any{}}, ref)
	if err == nil || client.movedTo != "" {
		t.Fatalf("empty destination must fail closed: moved=%q err=%v", client.movedTo, err)
	}

	client.destinationErr = errors.New("mailbox does not exist")
	_, err = mutateMessage(client, profile, connectors.PreparedAction{ActionName: ActionMoveMessage, Payload: map[string]any{"destination_folder": "Archive"}}, ref)
	if connectors.ErrorCode(err) != "folder_unavailable" || client.movedTo != "" {
		t.Fatalf("unavailable destination must be classified: moved=%q err=%v code=%q", client.movedTo, err, connectors.ErrorCode(err))
	}
}

func TestMutateMessageRejectsMissingUIDBeforeWriting(t *testing.T) {
	profile, err := profileConfigFrom(connectors.CredentialProfileView{Public: validProfilePublic()})
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	client := &fakeMutationClient{status: &imap.MailboxStatus{UidValidity: 7}, missing: true, moveSupport: true}
	_, err = mutateMessage(client, profile, connectors.PreparedAction{ActionName: ActionMarkRead}, messageRef{Folder: "INBOX", UIDValidity: 7, UID: 42})
	if err == nil || !strings.Contains(err.Error(), "no longer exists") || client.storeItem != "" || client.movedTo != "" {
		t.Fatalf("expected stale missing UID rejection, client=%#v err=%v", client, err)
	}
}
