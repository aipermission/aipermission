package keycleanup

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestVerificationSubjectsPreserveAliasesAndTrustWhileIgnoringLocalMetadata(t *testing.T) {
	entry := beginTest(t, New(newMemoryStore()), testIdentity(t))
	for _, change := range []string{"metadata", "alias", "port", "user", "material", "trust"} {
		t.Run(change, func(t *testing.T) {
			current := testIdentity(t)
			want := 2
			switch change {
			case "metadata":
				current.TargetID, current.TargetRevision = 10, "new revision"
				current.Profiles[0].ID, current.Profiles[0].KeyID = 20, 30
				want = 1
			case "alias":
				current.Host = "alias.test"
			case "port":
				current.Port++
			case "user":
				current.Username = "other"
			case "material":
				current.KeyDigest = testDigest(t, "other material")
			case "trust":
				current.HostFingerprints = []string{testFingerprint("other host")}
			}
			subjects, err := VerificationSubjects(entry.Record, current)
			if err != nil || len(subjects) != want {
				t.Fatalf("location %s was conflated or metadata duplicated it: %#v %v", change, subjects, err)
			}
			repeated, err := VerificationSubjects(entry.Record, current)
			if err != nil || !reflect.DeepEqual(subjects, repeated) {
				t.Fatal("verification identities are unstable")
			}
			subjects[0].HostFingerprints[0] = "mutated caller output"
			if entry.Record.Identity.HostFingerprints[0] != testFingerprint("host") {
				t.Fatal("returned verification subject aliases stored history")
			}
		})
	}
}

func TestVerificationSubjectUsesTheOriginalPublicKeyFingerprint(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	identity := testIdentity(t)
	material := sha256.Sum256(key.Marshal())
	identity.KeyDigest = hex.EncodeToString(material[:])
	entry := beginTest(t, New(newMemoryStore()), identity)
	subjects, err := VerificationSubjects(entry.Record, identity)
	if err != nil || len(subjects) != 1 || subjects[0].KeyFingerprint != ssh.FingerprintSHA256(key) {
		t.Fatalf("historical key fingerprint cannot identify the original key: %#v %v", subjects, err)
	}
	invalid := identity
	invalid.KeyDigest = "not a material digest"
	if _, err := VerificationSubjects(entry.Record, invalid); err == nil {
		t.Fatal("invalid key material produced a subject")
	}
	entry.Record.Version = 1
	if _, err := VerificationSubjects(entry.Record, identity); err == nil {
		t.Fatal("older attestation semantics were silently upgraded")
	}
}
