//go:build linux

package remotemetadata

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

type statProcessSession struct {
	ctx    context.Context
	cancel context.CancelFunc
	root   string
	stdout io.Writer
}

func (session *statProcessSession) SetStdout(output io.Writer) { session.stdout = output }

func (session *statProcessSession) Run(command string) error {
	process := exec.CommandContext(session.ctx, "/bin/sh", "-c", command)
	process.Dir = session.root
	process.Stdout = session.stdout
	return process.Run()
}

func (session *statProcessSession) Close() error { session.cancel(); return nil }

func TestReadNativeStatRetainsMetadataAndTreatsPathsAsData(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"plain", "-private", "user's report", "report ş界", "$(touch injected)", "item'; touch injected; echo '"} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(root, name)
			if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(file, 0o640); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				t.Fatalf("native stat type = %T", info.Sys())
			}
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			session := &statProcessSession{ctx: ctx, cancel: cancel, root: root}
			got, err := Read(ctx, session, name)
			want := Metadata{Mode: info.Mode(), UID: stat.Uid, GID: stat.Gid}
			if err != nil || got != want {
				t.Fatalf("native stat result = %#v, %v; want %#v", got, err, want)
			}
			if _, err := os.Stat(filepath.Join(root, "injected")); !os.IsNotExist(err) {
				t.Fatalf("path was interpreted as command syntax: %v", err)
			}
			if ctx.Err() != context.Canceled {
				t.Fatal("metadata reader did not close its owned command session")
			}
		})
	}
}
