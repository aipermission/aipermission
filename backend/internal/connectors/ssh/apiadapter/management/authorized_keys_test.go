package management

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveAuthorizedKeyCommandRemovesByPublicKeyBlob(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("create .ssh: %v", err)
	}
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITESTKEY aipermission-main"
	other := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOTHER other"
	authorizedKeys := strings.Join([]string{
		`from="127.0.0.1" ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITESTKEY custom-comment`,
		other,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(sshDir, "authorized_keys"), []byte(authorizedKeys), 0o600); err != nil {
		t.Fatalf("write authorized_keys: %v", err)
	}

	command := exec.Command("sh", "-c", removeAuthorizedKeyCommand(key))
	command.Env = append(os.Environ(), "HOME="+home)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("remove command failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "aipermission_key_removed=1") {
		t.Fatalf("expected removal count, got %s", output)
	}
	updated, err := os.ReadFile(filepath.Join(sshDir, "authorized_keys"))
	if err != nil {
		t.Fatalf("read authorized_keys: %v", err)
	}
	if strings.Contains(string(updated), "AAAAITESTKEY") {
		t.Fatalf("target key blob should be removed: %s", updated)
	}
	if !strings.Contains(string(updated), "AAAAIOTHER") {
		t.Fatalf("other key should remain: %s", updated)
	}
	info, err := os.Stat(filepath.Join(sshDir, "authorized_keys"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("authorized_keys mode = %o, want 600", info.Mode().Perm())
	}
}

func TestRemoveAuthorizedKeyCommandPreservesFileOnInterruptedCopy(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITESTKEY aipermission-main"
	contents := []byte(key + "\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOTHER other\n")
	keyPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(keyPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(home, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	catStub := "#!/bin/sh\ncase \"$1\" in\n  *.count) exec /bin/cat \"$@\" ;;\n  */authorized_keys.aipermission.*) printf 'partial'; exit 1 ;;\nesac\nexec /bin/cat \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "cat"), []byte(catStub), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "-c", removeAuthorizedKeyCommand(key))
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+binDir+":"+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("atomic key removal should not need a final copy: %v\n%s", err, output)
	}
	updated, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updated), "AAAAITESTKEY") || !strings.Contains(string(updated), "AAAAIOTHER") {
		t.Fatalf("unexpected key file after removal: %q", updated)
	}
}

func TestRemoveAuthorizedKeyCommandPreservesFileWhenRenameFails(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITESTKEY aipermission-main"
	contents := []byte(key + "\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOTHER other\n")
	keyPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(keyPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(home, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "mv"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "-c", removeAuthorizedKeyCommand(key))
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+binDir+":"+os.Getenv("PATH"))
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("expected publication failure, got %s", output)
	}
	updated, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != string(contents) {
		t.Fatalf("failed publication changed original keys: %q", updated)
	}
	if temps, err := filepath.Glob(filepath.Join(sshDir, ".authorized_keys.aipermission.*")); err != nil || len(temps) != 0 {
		t.Fatalf("failed publication left temporary files: %v, %v", temps, err)
	}
}

func TestRemoveAuthorizedKeyCommandCleansPartialTempWrite(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITESTKEY aipermission-main"
	contents := []byte(key + "\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOTHER other\n")
	keyPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(keyPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(home, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "awk"), []byte("#!/bin/sh\nprintf 'partial'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "-c", removeAuthorizedKeyCommand(key))
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+binDir+":"+os.Getenv("PATH"))
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("expected temporary write failure, got %s", output)
	}
	updated, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != string(contents) {
		t.Fatalf("temporary write failure changed original keys: %q", updated)
	}
	if temps, err := filepath.Glob(filepath.Join(sshDir, ".authorized_keys.*")); err != nil || len(temps) != 0 {
		t.Fatalf("temporary write failure left files: %v, %v", temps, err)
	}
}

func TestRemoveAuthorizedKeyCommandRejectsSymlink(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITESTKEY aipermission-main"
	contents := []byte(key + "\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOTHER other\n")
	linkedPath := filepath.Join(home, "linked-keys")
	if err := os.WriteFile(linkedPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkedPath, filepath.Join(sshDir, "authorized_keys")); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "-c", removeAuthorizedKeyCommand(key))
	command.Env = append(os.Environ(), "HOME="+home)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("expected symlink rejection, got %s", output)
	}
	updated, err := os.ReadFile(linkedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != string(contents) {
		t.Fatalf("symlink rejection changed destination keys: %q", updated)
	}
}

func TestTransportFailureMessageClassifiesCommonFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "auth failure",
			err:  errors.New("ssh dial: ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain"),
			want: "SSH authentication failed",
		},
		{
			name: "connection refused",
			err:  errors.New("ssh dial: dial tcp 192.0.2.10:22: connect: connection refused"),
			want: "SSH port refused",
		},
		{
			name: "timeout",
			err:  errors.New("ssh dial: dial tcp 192.0.2.10:22: i/o timeout"),
			want: "timed out",
		},
		{
			name: "unreachable",
			err:  errors.New("ssh dial: dial tcp 192.0.2.10:22: no route to host"),
			want: "not reachable",
		},
		{
			name: "host key",
			err:  errors.New("ssh dial: host key verification failed"),
			want: "host key verification failed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ConnectionFailureMessage(test.err)
			if !strings.Contains(got, test.want) {
				t.Fatalf("message %q does not contain %q", got, test.want)
			}
			if !strings.HasPrefix(got, "server connection test failed:") {
				t.Fatalf("connection test message should keep endpoint prefix, got %q", got)
			}
			commandMessage := CommandFailureMessage(test.err)
			if !strings.Contains(commandMessage, test.want) {
				t.Fatalf("command message %q does not contain %q", commandMessage, test.want)
			}
			if !strings.HasPrefix(commandMessage, "command execution failed:") {
				t.Fatalf("command message should keep exec prefix, got %q", commandMessage)
			}
		})
	}
}

func TestRemoveAuthorizedKeyCommandFailsWhenNoEntryRemoved(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatalf("create .ssh: %v", err)
	}
	command := exec.Command("sh", "-c", removeAuthorizedKeyCommand("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMISSING aipermission-main"))
	command.Env = append(os.Environ(), "HOME="+home)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("expected command to fail when no key is removed, got %s", output)
	}
	if !strings.Contains(string(output), "removed 0") {
		t.Fatalf("expected removed 0 message, got %s", output)
	}
}

func TestParseDockerPSOutput(t *testing.T) {
	output := `{"ID":"abc123","Image":"nginx:alpine","Command":"nginx -g daemon off;","CreatedAt":"2026-06-03 10:00:00 +0000 UTC","Names":"web","Status":"Up 2 minutes","State":"running","Ports":"0.0.0.0:8080->80/tcp","RunningFor":"2 minutes ago","Size":"1.2MB","Labels":"com.example=true","Mounts":"/data","Networks":"bridge"}`
	containers, available := parseDockerPSOutput(output)
	if !available {
		t.Fatalf("docker should be available")
	}
	if len(containers) != 1 {
		t.Fatalf("expected one container, got %#v", containers)
	}
	if containers[0].Name != "web" || containers[0].Image != "nginx:alpine" || containers[0].Command == "" || containers[0].Ports == "" || containers[0].Networks != "bridge" {
		t.Fatalf("unexpected container parse: %#v", containers[0])
	}

	containers, available = parseDockerPSOutput("__AIPERMISSION_DOCKER_UNAVAILABLE__")
	if available || len(containers) != 0 {
		t.Fatalf("docker unavailable marker should produce unavailable empty response")
	}
}

func TestDockerContainerRefValidationAndShellQuote(t *testing.T) {
	for _, value := range []string{"web", "abc123", "compose_service_1"} {
		if err := validateDockerContainerRef(value); err != nil {
			t.Fatalf("valid container ref failed: %s: %v", value, err)
		}
	}
	for _, value := range []string{"", "bad\nname", strings.Repeat("a", 129)} {
		if err := validateDockerContainerRef(value); err == nil {
			t.Fatalf("invalid container ref should fail: %q", value)
		}
	}
	if got := shellQuote("name'withquote"); got != `'name'\''withquote'` {
		t.Fatalf("unexpected shell quote: %s", got)
	}
	if normalizeDockerLogsTail(0) != 300 || normalizeDockerLogsTail(-5) != 300 {
		t.Fatalf("empty docker log tail should default to 300")
	}
	if normalizeDockerLogsTail(42) != 42 || normalizeDockerLogsTail(6000) != 5000 {
		t.Fatalf("docker log tail should preserve valid values and cap large values")
	}
}
