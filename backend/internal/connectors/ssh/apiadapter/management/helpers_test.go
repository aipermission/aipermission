package management

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net/http"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"golang.org/x/crypto/ssh"
)

func TestPresentUnknownHostKeyError(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	presentation, ok := PresentUnknownHostKeyError(fmt.Errorf("ssh dial: %w", execution.NewUnknownHostKeyError("[example.test]:22", key)))
	if !ok || presentation.StatusCode != http.StatusConflict {
		t.Fatalf("unknown host key presentation = %#v, %v", presentation, ok)
	}
	payload, ok := presentation.Payload.(unknownHostKeyResponse)
	if !ok || payload.Code != "unknown_ssh_host_key" || payload.HostKey.Host != "example.test" || payload.HostKey.Port != 22 {
		t.Fatalf("unknown host key payload = %#v", presentation.Payload)
	}
}
