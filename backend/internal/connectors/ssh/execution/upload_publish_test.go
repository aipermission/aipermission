package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestMCPUploadRejectsChangedStagingBeforeDial(t *testing.T) {
	file := filepath.Join(t.TempDir(), "staging")
	if err := os.WriteFile(file, []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("approved"))
	_, err := UploadFileWithOptions(t.Context(), Target{}, file, "/example", false, TransferOptions{
		ExpectedContent: &connectors.FileContentIdentity{SizeBytes: 8, ChecksumSHA256: hex.EncodeToString(digest[:])},
	})
	if !errors.Is(err, connectors.ErrUploadContentChanged) {
		t.Fatalf("changed staged bytes reached SSH dial: %v", err)
	}
}

func TestMCPUploadDoesNotSendBytesBeyondApprovedLength(t *testing.T) {
	for _, size := range []int64{0, 3} {
		copied, _, err := copyWithProgress(t.Context(), io.Discard, strings.NewReader("four"), size, TransferOptions{
			ExpectedContent: &connectors.FileContentIdentity{SizeBytes: size},
		})
		if !errors.Is(err, connectors.ErrUploadContentChanged) || copied != 0 {
			t.Fatalf("size %d: transmitted %d bytes before rejecting %v", size, copied, err)
		}
	}
}

func TestMCPUploadRechecksAuthorizationAfterRemotePreflight(t *testing.T) {
	const destination = "/tmp/example"
	for _, overwrite := range []bool{false, true} {
		client := &fakeUploadCommitter{existing: map[string]bool{destination: overwrite}}
		preflight := &uploadCommitProbe{fakeUploadCommitter: client}
		denied := errors.New("permission expired")
		err := publishRemoteUpload(t.Context(), preflight, io.NopCloser(strings.NewReader("")), "/tmp/staged", destination, overwrite,
			func(context.Context) (context.Context, func(), error) {
				if !preflight.checked {
					t.Error("authorization ran before destination preflight")
				}
				return nil, nil, denied
			},
		)
		if !errors.Is(err, denied) || len(client.links)+len(client.posixRenames)+len(client.removes) != 0 {
			t.Fatalf("remote mutation ran after denial: %v", err)
		}
	}
}

type uploadCommitProbe struct {
	*fakeUploadCommitter
	checked bool
}

func (probe *uploadCommitProbe) Lstat(path string) (os.FileInfo, error) {
	probe.checked = true
	return probe.fakeUploadCommitter.Lstat(path)
}
