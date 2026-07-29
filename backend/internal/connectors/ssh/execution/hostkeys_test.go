package execution

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestHostKeyRequiresExplicitTrustBeforeFirstUse(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	hostKey, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatalf("ssh public key: %v", err)
	}

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
	publicA, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate first key: %v", err)
	}
	firstKey, err := ssh.NewPublicKey(publicA)
	if err != nil {
		t.Fatalf("first ssh public key: %v", err)
	}
	publicB, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate second key: %v", err)
	}
	secondKey, err := ssh.NewPublicKey(publicB)
	if err != nil {
		t.Fatalf("second ssh public key: %v", err)
	}

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
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
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
