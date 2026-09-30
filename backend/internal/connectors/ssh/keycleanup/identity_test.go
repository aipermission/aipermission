package keycleanup

import (
	"reflect"
	"testing"
)

func TestIdentityCanonicalizesWithoutMutatingInputs(t *testing.T) {
	input := testIdentity(t)
	input.Host, input.Username = " [2001:0DB8::1] ", " operator "
	second := input.Profiles[0]
	second.ID++
	input.Profiles = append([]ProfileIdentity{second}, input.Profiles...)
	input.HostFingerprints = []string{testFingerprint("z"), testFingerprint("a"), testFingerprint("z")}
	originalProfiles := append([]ProfileIdentity{}, input.Profiles...)
	originalTrust := append([]string{}, input.HostFingerprints...)
	identity, err := NewIdentity(input)
	if err != nil || identity.Host != "2001:db8::1" || identity.Username != "operator" || len(identity.HostFingerprints) != 2 || identity.Profiles[0].ID != 2 {
		t.Fatalf("canonical identity = %#v, %v", identity, err)
	}
	if !reflect.DeepEqual(input.Profiles, originalProfiles) || !reflect.DeepEqual(input.HostFingerprints, originalTrust) {
		t.Fatal("identity mutated caller's snapshot")
	}
	canonical, err := NewIdentity(identity)
	if err != nil || !reflect.DeepEqual(canonical, identity) {
		t.Fatalf("non-idempotent canonicalization: %#v, %v", canonical, err)
	}
	if _, err := Digest(func() {}); err == nil {
		t.Fatal("unsupported digest input accepted")
	}
}

func TestIdentityRejectsIncompleteOrAmbiguousSnapshots(t *testing.T) {
	mutations := map[string]func(*Identity){
		"target":             func(i *Identity) { i.TargetID = 0 },
		"target revision":    func(i *Identity) { i.TargetRevision = "" },
		"config digest":      func(i *Identity) { i.ConfigDigest = "bad" },
		"host":               func(i *Identity) { i.Host = "" },
		"port":               func(i *Identity) { i.Port = 65536 },
		"username":           func(i *Identity) { i.Username = "" },
		"key digest":         func(i *Identity) { i.KeyDigest = "bad" },
		"missing trust":      func(i *Identity) { i.HostFingerprints = nil },
		"malformed trust":    func(i *Identity) { i.HostFingerprints = []string{"SHA256:invalid"} },
		"missing profiles":   func(i *Identity) { i.Profiles = nil },
		"duplicate profiles": func(i *Identity) { i.Profiles = append(i.Profiles, i.Profiles[0]) },
		"profile id":         func(i *Identity) { i.Profiles[0].ID = 0 },
		"profile revision":   func(i *Identity) { i.Profiles[0].Revision = "" },
		"secret revision":    func(i *Identity) { i.Profiles[0].SecretRevision = "" },
		"profile public":     func(i *Identity) { i.Profiles[0].PublicDigest = "bad" },
		"key id":             func(i *Identity) { i.Profiles[0].KeyID = 0 },
		"key revision":       func(i *Identity) { i.Profiles[0].KeyRevision = "" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			identity := testIdentity(t)
			mutate(&identity)
			if _, err := NewIdentity(identity); err == nil {
				t.Fatal("invalid identity accepted")
			}
		})
	}
}
