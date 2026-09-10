package actions

import (
	"strings"
	"testing"
)

func TestIdentityTagsAreKeyedAndWorkspaceBound(t *testing.T) {
	canonical := []byte(`{"input":{"password":"guessable"}}`)
	firstKey, err := DeriveIdentityKey("gateway-secret-one", "workspace-one")
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := DeriveIdentityKey("gateway-secret-two", "workspace-one")
	if err != nil {
		t.Fatal(err)
	}
	otherWorkspaceKey, err := DeriveIdentityKey("gateway-secret-one", "workspace-two")
	if err != nil {
		t.Fatal(err)
	}
	first, err := IdentityTag(firstKey, canonical)
	if err != nil {
		t.Fatal(err)
	}
	second, err := IdentityTag(secondKey, canonical)
	if err != nil {
		t.Fatal(err)
	}
	otherWorkspace, err := IdentityTag(otherWorkspaceKey, canonical)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, identityVersion) || first == second || first == otherWorkspace {
		t.Fatalf("identity tags are not independently keyed: first=%q second=%q workspace=%q", first, second, otherWorkspace)
	}
	if strings.Contains(first, "guessable") {
		t.Fatal("identity tag exposed canonical request content")
	}
}

func TestIdentityTagFailsClosedWithoutRuntimeKey(t *testing.T) {
	if _, err := IdentityTag(nil, []byte("request")); err == nil {
		t.Fatal("missing identity key was accepted")
	}
}

func TestClearIdentityKeyZeroesCallerStorage(t *testing.T) {
	key := []byte("sensitive")
	ClearIdentityKey(key)
	for _, value := range key {
		if value != 0 {
			t.Fatal("identity key storage was not cleared")
		}
	}
}
