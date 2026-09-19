package execution

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestHostKeyRequiresExplicitTrustBeforeFirstUse(t *testing.T) {
	hostKey := generateHostKey(t)

	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	callback, err := HostKeyCallback(knownHostsPath)
	if err != nil {
		t.Fatalf("host key callback: %v", err)
	}

	var unknown *UnknownHostKeyError
	if err := callback("[example.test]:2222", nil, hostKey); !errors.As(err, &unknown) {
		t.Fatalf("expected unknown host key error, got %T: %v", err, err)
	}
	if unknown.FingerprintSHA256 != HostKeyFingerprintSHA256(hostKey) {
		t.Fatalf("unexpected fingerprint: %q", unknown.FingerprintSHA256)
	}

	if err := TrustHostKey(knownHostsPath, "[example.test]:2222", unknown.PublicKey); err != nil {
		t.Fatalf("trust host key: %v", err)
	}
	if err := callback("[example.test]:2222", nil, hostKey); err != nil {
		t.Fatalf("trusted host key should pass: %v", err)
	}
	fingerprints, err := TrustedHostFingerprints(knownHostsPath, "[example.test]:2222")
	if err != nil {
		t.Fatalf("list trusted host fingerprints: %v", err)
	}
	if len(fingerprints) != 1 || fingerprints[0] != HostKeyFingerprintSHA256(hostKey) {
		t.Fatalf("unexpected trusted fingerprints: %#v", fingerprints)
	}
}

func TestHostKeyChangeRequiresExplicitReplacement(t *testing.T) {
	firstKey := generateHostKey(t)
	secondKey := generateHostKey(t)

	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	hostname := "[example.test]:2222"
	if err := TrustHostKey(knownHostsPath, hostname, NewUnknownHostKeyError(hostname, firstKey).PublicKey); err != nil {
		t.Fatalf("trust first host key: %v", err)
	}
	callback, err := HostKeyCallback(knownHostsPath)
	if err != nil {
		t.Fatalf("host key callback: %v", err)
	}

	var changed *ChangedHostKeyError
	if err := callback(hostname, nil, secondKey); !errors.As(err, &changed) {
		t.Fatalf("expected changed host key error, got %T: %v", err, err)
	}
	if changed.FingerprintSHA256 != HostKeyFingerprintSHA256(secondKey) {
		t.Fatalf("unexpected new fingerprint: %q", changed.FingerprintSHA256)
	}
	if len(changed.ExistingFingerprints) != 1 || changed.ExistingFingerprints[0] != HostKeyFingerprintSHA256(firstKey) {
		t.Fatalf("unexpected existing fingerprints: %#v", changed.ExistingFingerprints)
	}

	if err := ReplaceHostKey(knownHostsPath, hostname, changed.PublicKey); err != nil {
		t.Fatalf("replace host key: %v", err)
	}
	if err := callback(hostname, nil, secondKey); err != nil {
		t.Fatalf("replaced host key should pass: %v", err)
	}
	var changedAgain *ChangedHostKeyError
	if err := callback(hostname, nil, firstKey); !errors.As(err, &changedAgain) {
		t.Fatalf("old host key should now be rejected, got %T: %v", err, err)
	}
}

func TestHostKeyReplacementValidatesBeforeChangingTrustFile(t *testing.T) {
	hostKey := generateHostKey(t)
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	hostname := "[example.test]:2222"
	if err := TrustHostKey(knownHostsPath, hostname, NewUnknownHostKeyError(hostname, hostKey).PublicKey); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(knownHostsPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := ReplaceHostKey(knownHostsPath, hostname, "not-an-ssh-public-key"); err == nil {
		t.Fatal("expected invalid replacement key to fail")
	}
	after, err := os.ReadFile(knownHostsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("invalid replacement changed known_hosts:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestReplaceHostKeyNormalizesRouteAddresses(t *testing.T) {
	firstKey := generateHostKey(t)
	secondKey := generateHostKey(t)
	for _, test := range []struct {
		name string
		host string
		port int
	}{
		{name: "dns default", host: "example.test", port: 22},
		{name: "dns custom", host: "example.test", port: 2222},
		{name: "ipv4 default", host: "192.0.2.10", port: 22},
		{name: "ipv4 custom", host: "192.0.2.10", port: 2222},
		{name: "ipv6 default", host: "2001:db8::10", port: 22},
		{name: "ipv6 custom", host: "2001:db8::10", port: 2222},
	} {
		t.Run(test.name, func(t *testing.T) {
			hostname := net.JoinHostPort(test.host, strconv.Itoa(test.port))
			path := filepath.Join(t.TempDir(), "known_hosts")
			if err := TrustHostKey(path, hostname, NewUnknownHostKeyError(hostname, firstKey).PublicKey); err != nil {
				t.Fatal(err)
			}
			if err := ReplaceHostKey(path, hostname, NewUnknownHostKeyError(hostname, secondKey).PublicKey); err != nil {
				t.Fatal(err)
			}
			assertHostKeyAccepted(t, path, hostname, secondKey)
			assertHostKeyRejected(t, path, hostname, firstKey)
		})
	}
}

func TestReplaceHostKeyPreservesOtherPortsAndHosts(t *testing.T) {
	oldKey := generateHostKey(t)
	newKey := generateHostKey(t)
	otherKey := generateHostKey(t)
	target := net.JoinHostPort("example.test", "2222")
	defaultPort := net.JoinHostPort("example.test", "22")
	otherHost := net.JoinHostPort("other.test", "2222")
	path := filepath.Join(t.TempDir(), "known_hosts")
	data := knownhosts.Line([]string{target, otherHost}, oldKey) + "\n" + knownhosts.Line([]string{defaultPort}, otherKey) + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceHostKey(path, target, NewUnknownHostKeyError(target, newKey).PublicKey); err != nil {
		t.Fatal(err)
	}
	assertHostKeyAccepted(t, path, target, newKey)
	assertHostKeyRejected(t, path, target, oldKey)
	assertHostKeyAccepted(t, path, otherHost, oldKey)
	assertHostKeyAccepted(t, path, defaultPort, otherKey)
}

func TestReplaceHostKeyRemovesHashedExactEntry(t *testing.T) {
	oldKey := generateHostKey(t)
	newKey := generateHostKey(t)
	target := net.JoinHostPort("example.test", "2222")
	path := filepath.Join(t.TempDir(), "known_hosts")
	hashed := knownhosts.HashHostname(knownhosts.Normalize(target))
	if err := os.WriteFile(path, []byte(knownhosts.Line([]string{hashed}, oldKey)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceHostKey(path, target, NewUnknownHostKeyError(target, newKey).PublicKey); err != nil {
		t.Fatal(err)
	}
	assertHostKeyAccepted(t, path, target, newKey)
	assertHostKeyRejected(t, path, target, oldKey)
}

func TestReplaceHostKeyRejectsWildcardThatWouldKeepPriorKey(t *testing.T) {
	oldKey := generateHostKey(t)
	newKey := generateHostKey(t)
	target := net.JoinHostPort("node.example.test", "22")
	path := filepath.Join(t.TempDir(), "known_hosts")
	original := knownhosts.Line([]string{"*.example.test"}, oldKey) + "\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceHostKey(path, target, NewUnknownHostKeyError(target, newKey).PublicKey); err == nil {
		t.Fatal("expected ambiguous wildcard replacement to fail")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Fatalf("failed replacement changed trust file: %q", after)
	}
}

func TestReplaceHostKeyPreservesRevocationAlongsideWildcardTrust(t *testing.T) {
	revokedKey := generateHostKey(t)
	replacementKey := generateHostKey(t)
	target := net.JoinHostPort("node.example.test", "22")
	path := filepath.Join(t.TempDir(), "known_hosts")
	revokedLine := "@revoked " + knownhosts.Line([]string{target}, revokedKey)
	data := revokedLine + "\n" + knownhosts.Line([]string{"*.example.test"}, revokedKey) + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceHostKey(path, target, NewUnknownHostKeyError(target, replacementKey).PublicKey); err != nil {
		t.Fatalf("replace host key: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), revokedLine+"\n") {
		t.Fatalf("replacement removed revoked marker: %q", after)
	}
	assertHostKeyAccepted(t, path, target, replacementKey)
	assertHostKeyRejected(t, path, target, revokedKey)
}

func TestReplaceKnownHostDataPreservesMarkerRecords(t *testing.T) {
	oldKey := generateHostKey(t)
	newKey := generateHostKey(t)
	target := net.JoinHostPort("node.example.test", "22")
	marker := "@cert-authority " + knownhosts.Line([]string{target}, oldKey)
	ordinary := knownhosts.Line([]string{target}, oldKey)

	result := string(replaceKnownHostData(
		[]byte(marker+"\n"+ordinary+"\n"),
		target,
		knownhosts.Line([]string{target}, newKey),
	))
	if !strings.Contains(result, marker+"\n") {
		t.Fatalf("replacement removed certificate-authority marker: %q", result)
	}
	for _, line := range strings.Split(result, "\n") {
		if line == ordinary {
			t.Fatalf("replacement retained ordinary exact-host record: %q", result)
		}
	}
}

func generateHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func assertHostKeyAccepted(t *testing.T, path, hostname string, key ssh.PublicKey) {
	t.Helper()
	callback, err := HostKeyCallback(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := callback(hostname, nil, key); err != nil {
		t.Fatalf("expected host key to be accepted: %v", err)
	}
}

func assertHostKeyRejected(t *testing.T, path, hostname string, key ssh.PublicKey) {
	t.Helper()
	callback, err := HostKeyCallback(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := callback(hostname, nil, key); err == nil {
		t.Fatal("expected host key to be rejected")
	}
}
