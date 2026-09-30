package management

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestRemoveAuthorizedKeyAcceptsSupportedQuotedOptionsAndCountsDuplicates(t *testing.T) {
	target := identityTestPublicKey(t)
	for _, options := range []string{
		"restrict,no-pty,no-agent-forwarding",
		"cert-authority,principals=\"one,two\"",
		"command=\"hello \\\\\" world\",environment=\"NAME=hello,world\"",
		"from=\"*.example.test\",expiry-time=\"20301231Z\"",
		"permitopen=\"localhost:80\",permitlisten=\"8080\",tunnel=\"1\"",
		"port-forwarding,x11-forwarding,no-touch-required,verify-required,user-rc",
	} {
		t.Run(options, func(t *testing.T) {
			line := options + " " + target
			updated, output := runKeyIdentityRemover(t, target, line+"\n"+target+"\n")
			if updated != "" || output != "aipermission_key_removed=2\n" {
				t.Fatalf("supported options did not revoke both keys: %q %q", updated, output)
			}
		})
	}
}

func TestAuthorizedKeyEscapeFixturesAgreeWithOpenSSH(t *testing.T) {
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("OpenSSH fixture oracle requires ssh-keygen")
	}
	target, other := identityTestPublicKey(t), identityTestPublicKey(t)
	for _, fixture := range []struct{ line, key string }{
		{`command="hello \\" ssh-ed25519 ` + publicKeyBlob(target) + ` \" end" ` + other, other},
		{`command="hello \\" world" ` + target, target},
	} {
		t.Run(fixture.line, func(t *testing.T) {
			key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(fixture.key))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "authorized_keys")
			if err := os.WriteFile(path, []byte(fixture.line+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			output, err := exec.CommandContext(t.Context(), keygen, "-l", "-f", path).CombinedOutput()
			if err != nil || !bytes.Contains(output, []byte(ssh.FingerprintSHA256(key))) {
				t.Fatalf("OpenSSH disagrees with fixture identity: %v %q", err, output)
			}
		})
	}
}

func TestRemoveAuthorizedKeyDoesNotParseOtherAlgorithmComments(t *testing.T) {
	target := identityTestPublicKey(t)
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public, err := ssh.NewPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	other := string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(public)))
	for _, comment := range []string{`alice"host`, "alice\rhost", target} {
		t.Run(comment, func(t *testing.T) {
			preserved := other + " " + comment + "\n"
			updated, output := runKeyIdentityRemover(t, target, preserved+target+"\n")
			if updated != preserved || output != "aipermission_key_removed=1\n" {
				t.Fatalf("unrelated comment blocked cleanup or changed: %q %q", updated, output)
			}
		})
	}
}
