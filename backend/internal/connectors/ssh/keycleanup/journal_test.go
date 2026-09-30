package keycleanup

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestJournalConfirmsOnlyExactSnapshotWithoutReadingSecrets(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	journal := New(store)
	identity := testIdentity(t)
	intent := beginTest(t, journal, identity)
	if _, dispatch, err := New(store).Begin(ctx, identity); dispatch || !errors.Is(err, ErrReconciliationRequired) {
		t.Fatalf("reopened uncertain attempt dispatched: %t, %v", dispatch, err)
	}
	confirmed, err := journal.Confirm(ctx, intent)
	if err != nil || confirmed.Record.Status != Confirmed || confirmed.Record.Generation == intent.Record.Generation {
		t.Fatalf("confirmation = %#v, %v", confirmed, err)
	}
	resumed, dispatch, err := New(store).Begin(ctx, identity)
	if err != nil || dispatch || !reflect.DeepEqual(resumed, confirmed) {
		t.Fatalf("confirmed retry = %#v, dispatch %t, %v", resumed, dispatch, err)
	}
	if _, err := journal.Confirm(ctx, intent); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("old confirmation was accepted: %v", err)
	}
	if _, err := journal.Confirm(ctx, confirmed); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("duplicate confirmation was accepted: %v", err)
	}
	if store.secretReads != 0 || len(store.rows) != 1 {
		t.Fatalf("journal secret reads %d, rows %d", store.secretReads, len(store.rows))
	}
}

func TestJournalFencesChangedOrAliasedIdentityAfterConfirmedRevocation(t *testing.T) {
	mutations := map[string]func(*Identity){
		"target revision":              func(i *Identity) { i.TargetRevision = "v2" },
		"config same revision":         func(i *Identity) { i.ConfigDigest = testDigest(t, "different") },
		"profile revision":             func(i *Identity) { i.Profiles[0].Revision = "v2" },
		"profile public same revision": func(i *Identity) { i.Profiles[0].PublicDigest = testDigest(t, "different") },
		"secret revision":              func(i *Identity) { i.Profiles[0].SecretRevision = "2" },
		"key alias":                    func(i *Identity) { i.Profiles[0].KeyID++ },
		"key revision":                 func(i *Identity) { i.Profiles[0].KeyRevision = "v2" },
		"profile added":                func(i *Identity) { p := i.Profiles[0]; p.ID++; i.Profiles = append(i.Profiles, p) },
		"host changed":                 func(i *Identity) { i.Host = "new.test" },
		"trust replaced":               func(i *Identity) { i.HostFingerprints = []string{testFingerprint("replacement")} },
		"target alias":                 func(i *Identity) { i.TargetID++ },
		"endpoint alias":               func(i *Identity) { i.TargetID++; i.Host = "alias.test"; i.Profiles[0].ID++ },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			identity := testIdentity(t)
			journal := New(newMemoryStore())
			entry := beginTest(t, journal, identity)
			if _, err := journal.Confirm(context.Background(), entry); err != nil {
				t.Fatal(err)
			}
			changed := testIdentity(t)
			mutate(&changed)
			changed, err := NewIdentity(changed)
			if err != nil {
				t.Fatal(err)
			}
			if _, dispatch, err := journal.Begin(context.Background(), changed); dispatch || !errors.Is(err, ErrReconciliationRequired) {
				t.Fatalf("changed identity dispatched: %t, %v", dispatch, err)
			}
		})
	}
}

func TestJournalKeepsIndependentEndpointAndKeyWorkSeparate(t *testing.T) {
	journal := New(newMemoryStore())
	beginTest(t, journal, testIdentity(t))
	other := testIdentity(t)
	other.TargetID, other.Profiles[0].ID = 10, 20
	other.Host, other.HostFingerprints = "other.test", []string{testFingerprint("other")}
	beginTest(t, journal, other)
	other = testIdentity(t)
	other.KeyDigest = testDigest(t, "other-public-key")
	other.Profiles[0].KeyID++
	beginTest(t, journal, other)
	other = testIdentity(t)
	other.Username = "another-user"
	beginTest(t, journal, other)
}

func TestJournalAttestationIsGenerationAndCurrentSnapshotBound(t *testing.T) {
	ctx := context.Background()
	journal := New(newMemoryStore())
	identity := testIdentity(t)
	intent := beginTest(t, journal, identity)
	current := testIdentity(t)
	current.Profiles[0].KeyID++
	attested, err := journal.Attest(ctx, intent, current, " Verified absence in the provider console ")
	if err != nil || attested.Record.Status != Attested || len(attested.Record.Attestations) != 1 || attested.Record.Attestations[0].Reason != "Verified absence in the provider console" {
		t.Fatalf("attestation = %#v, %v", attested, err)
	}
	if _, dispatch, err := journal.Begin(ctx, current); err != nil || dispatch {
		t.Fatalf("verified current snapshot dispatched: %t, %v", dispatch, err)
	}
	if _, dispatch, err := journal.Begin(ctx, identity); dispatch || !errors.Is(err, ErrReconciliationRequired) {
		t.Fatalf("attestation applied to old snapshot: %t, %v", dispatch, err)
	}
	if _, err := journal.Attest(ctx, intent, current, "repeat"); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("stale attestation = %v", err)
	}
	current.TargetRevision = "another snapshot"
	if _, dispatch, err := journal.Begin(ctx, current); dispatch || !errors.Is(err, ErrReconciliationRequired) {
		t.Fatalf("attestation applied to later snapshot: %t, %v", dispatch, err)
	}
}

func TestJournalAttestationRejectsUnrelatedIdentityOrMissingReason(t *testing.T) {
	journal := New(newMemoryStore())
	intent := beginTest(t, journal, testIdentity(t))
	current := testIdentity(t)
	current.KeyDigest = testDigest(t, "another key")
	if _, err := journal.Attest(context.Background(), intent, current, "confirmed"); err == nil {
		t.Fatal("unrelated key was attested")
	}
	for _, reason := range []string{"", "   ", string(make([]byte, 2001))} {
		if _, err := journal.Attest(context.Background(), intent, testIdentity(t), reason); err == nil {
			t.Fatal("invalid reason was accepted")
		}
	}
}
