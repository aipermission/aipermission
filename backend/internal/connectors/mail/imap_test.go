package mailconnector

import (
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-imap"
)

type fakeMailboxSelector struct {
	name     string
	readOnly bool
	status   *imap.MailboxStatus
	err      error
}

func (selector *fakeMailboxSelector) Select(name string, readOnly bool) (*imap.MailboxStatus, error) {
	selector.name = name
	selector.readOnly = readOnly
	return selector.status, selector.err
}

func TestExamineMailboxAlwaysSelectsReadOnly(t *testing.T) {
	selector := &fakeMailboxSelector{status: &imap.MailboxStatus{UidValidity: 7}}
	status, err := examineMailbox(selector, "INBOX")
	if err != nil || status.UidValidity != 7 || selector.name != "INBOX" || !selector.readOnly {
		t.Fatalf("EXAMINE contract changed: selector=%#v status=%#v err=%v", selector, status, err)
	}

	selector.err = errors.New("refused")
	if _, err := examineMailbox(selector, "INBOX"); err == nil || !strings.Contains(err.Error(), "IMAP EXAMINE") {
		t.Fatalf("EXAMINE failure was not classified: %v", err)
	}
}

func TestEnvelopeRowBoundsServerControlledFlags(t *testing.T) {
	flags := make([]string, maxServerMetadataRows+10)
	for index := range flags {
		flags[index] = strings.Repeat("x", maxServerMetadataBytes+10)
	}
	row := envelopeRow("INBOX", 7, &imap.Message{
		Uid:      42,
		Envelope: &imap.Envelope{},
		Flags:    flags,
	})
	bounded := row["flags"].([]string)
	if len(bounded) != maxServerMetadataRows || len(bounded[0]) != maxServerMetadataBytes {
		t.Fatalf("flags were not bounded: count=%d first_bytes=%d", len(bounded), len(bounded[0]))
	}
}
