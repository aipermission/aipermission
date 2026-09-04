package api

import (
	"strings"
	"testing"
)

func TestConnectorActionIdentityTagsAreKeyedAndWorkspaceBound(t *testing.T) {
	canonical := []byte(`{"input":{"password":"guessable"}}`)
	firstKey, err := deriveConnectorActionIdentityKey("gateway-secret-one", "workspace-one")
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := deriveConnectorActionIdentityKey("gateway-secret-two", "workspace-one")
	if err != nil {
		t.Fatal(err)
	}
	otherWorkspaceKey, err := deriveConnectorActionIdentityKey("gateway-secret-one", "workspace-two")
	if err != nil {
		t.Fatal(err)
	}
	first, err := connectorActionIdentityTag(firstKey, canonical)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := connectorActionIdentityTag(secondKey, canonical)
	otherWorkspace, _ := connectorActionIdentityTag(otherWorkspaceKey, canonical)
	if !strings.HasPrefix(first, connectorActionIdentityVersion) || first == second || first == otherWorkspace {
		t.Fatalf("identity tags are not independently keyed: first=%q second=%q workspace=%q", first, second, otherWorkspace)
	}
	if strings.Contains(first, "guessable") {
		t.Fatal("identity tag exposed canonical request content")
	}
}

func TestConnectorActionIdentityTagFailsClosedWithoutRuntimeKey(t *testing.T) {
	if _, err := connectorActionIdentityTag(nil, []byte("request")); err == nil {
		t.Fatal("missing identity key was accepted")
	}
}
