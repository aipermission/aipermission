package management

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestRemoveAuthorizedKeyMatchesMaterialNotCommentsOrQuotedOptions(t *testing.T) {
	target, other := identityTestPublicKey(t), identityTestPublicKey(t)
	blob := publicKeyBlob(target)
	for _, preserved := range []string{
		other + " " + blob,
		other + " " + target,
		`command="printf ssh-ed25519 ` + blob + `",no-pty ` + other,
		`command="printf \"ssh-ed25519 ` + blob + `\"",no-pty ` + other,
		`command="hello \\" ssh-ed25519 ` + blob + ` \" end" ` + other,
		"# " + target,
	} {
		t.Run(preserved, func(t *testing.T) {
			contents := preserved + "\n" + `command="printf hello world",no-pty ` + target + "\n"
			updated, output := runKeyIdentityRemover(t, target, contents)
			if updated != preserved+"\n" || output != "aipermission_key_removed=1\n" {
				t.Fatalf("removed unrelated identity or missed target: file %q output %q", updated, output)
			}
		})
	}
}

func TestRemoveAuthorizedKeyConfirmsAbsenceWithoutChangingCommentOnlyMatches(t *testing.T) {
	target := identityTestPublicKey(t)
	contents := identityTestPublicKey(t) + " comment " + publicKeyBlob(target) + "\n"
	updated, output := runKeyIdentityRemover(t, target, contents)
	if updated != contents || output != "aipermission_key_removed=0\n" {
		t.Fatalf("comment match changed unrelated key: file %q output %q", updated, output)
	}
}

func TestRemoveAuthorizedKeyUsesOpenSSHQuoteAndWhitespaceBoundaries(t *testing.T) {
	target := identityTestPublicKey(t)
	for _, line := range []string{
		`command="hello \\" world" ` + target,
		"\tno-pty\t" + target + "\r",
		target + "\r",
	} {
		t.Run(line, func(t *testing.T) {
			parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
			if err != nil || string(ssh.MarshalAuthorizedKey(parsed)) != target+"\n" {
				t.Fatalf("fixture does not identify the target: %v", err)
			}
			updated, output := runKeyIdentityRemover(t, target, line+"\n")
			if updated != "" || output != "aipermission_key_removed=1\n" {
				t.Fatalf("target retained or miscounted: %q %q", updated, output)
			}
		})
	}
}

func TestRemoveAuthorizedKeyRejectsAmbiguousSyntaxWithoutPublishing(t *testing.T) {
	target := identityTestPublicKey(t)
	for _, line := range []string{
		"unknown-option " + target,
		"command=oops " + target,
		"ssh-ed25519\r" + publicKeyBlob(target),
		`command="unterminated ` + target,
		"no-pty, " + target,
	} {
		t.Run(line, func(t *testing.T) {
			contents := target + "\n" + line + "\n"
			updated, output, err := executeKeyIdentityRemover(t, target, contents)
			if err == nil || updated != contents || strings.Contains(output, "aipermission_key_removed=") {
				t.Fatalf("ambiguous file was published or confirmed: err %v file %q output %q", err, updated, output)
			}
		})
	}
}

func identityTestPublicKey(t *testing.T) string {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}

func runKeyIdentityRemover(t *testing.T, publicKey, contents string) (string, string) {
	t.Helper()
	updated, output, err := executeKeyIdentityRemover(t, publicKey, contents)
	if err != nil {
		t.Fatalf("key removal failed: %v %q", err, output)
	}
	return updated, output
}

func executeKeyIdentityRemover(t *testing.T, publicKey, contents string) (string, string, error) {
	t.Helper()
	home := t.TempDir()
	directory := filepath.Join(home, ".ssh")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "authorized_keys")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "sh", "-c", removeAuthorizedKeyCommand(publicKey))
	command.Env = append(os.Environ(), "HOME="+home)
	output, err := command.CombinedOutput()
	updated, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(updated), string(output), err
}
