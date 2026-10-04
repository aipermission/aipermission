package gatewayinfrastructure

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestNativeInfrastructureAccessStaysHandleAndComponentScoped(t *testing.T) {
	component := NewComponent(filepath.Join(t.TempDir(), "selection.aipdb"), nil)
	first := openNativeInfrastructureHandle(t, component, "first-\u03b1")
	second := openNativeInfrastructureHandle(t, component, "second-\u03b2")
	foreign := NewComponent(filepath.Join(t.TempDir(), "foreign.aipdb"), nil)
	access := component.AccessOwner()
	initial, err := access.ReadSecuritySettings(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	otherInitial, err := access.ReadSecuritySettings(t.Context(), second)
	if err != nil || initial != otherInitial {
		t.Fatalf("initial native controls differ: %#v %#v %v", initial, otherInitial, err)
	}
	beforeFirst, beforeSecond := infrastructureOutboxCount(t, first), infrastructureOutboxCount(t, second)
	want := initial
	want.ReusableTokens = !initial.ReusableTokens
	want.ExposeMCPServerMetadata = !initial.ExposeMCPServerMetadata
	if got, err := access.UpdateSecuritySettings(t.Context(), first, want); err != nil || got != want {
		t.Fatalf("actual audited settings mutation: %#v %v", got, err)
	}
	if got, err := access.ReadSecuritySettings(t.Context(), first); err != nil || got != want {
		t.Fatalf("first native settings did not persist: %#v %v", got, err)
	}
	if got, err := access.ReadSecuritySettings(t.Context(), second); err != nil || got != otherInitial {
		t.Fatalf("settings crossed workspace: %#v %v", got, err)
	}
	if infrastructureDurableSettings(t, first) != want || infrastructureDurableSettings(t, second) != otherInitial {
		t.Fatal("cached isolation did not match durable workspace settings")
	}
	if infrastructureOutboxCount(t, first) != beforeFirst+1 || infrastructureOutboxCount(t, second) != beforeSecond {
		t.Fatal("audited mutation crossed workspace or omitted the required event")
	}
	if !access.SetMCPStarted(first, true) || !access.SetMCPStarted(second, false) {
		t.Fatal("native control capability unavailable")
	}
	if started, ok := access.MCPStarted(first); !ok || !started {
		t.Fatal("first MCP control not set")
	}
	if started, ok := access.MCPStarted(second); !ok || started {
		t.Fatal("MCP control crossed workspace")
	}
	firstTokens, firstOK := access.MCPTokenSource(first)
	secondTokens, secondOK := access.MCPTokenSource(second)
	if !firstOK || !secondOK || firstTokens == nil || secondTokens == nil || firstTokens == secondTokens {
		t.Fatal("native token sources are missing or aliased")
	}
	if _, err := foreign.AccessOwner().ReadSecuritySettings(t.Context(), first); !errors.Is(err, ErrWorkspaceHandleUnavailable) {
		t.Fatalf("foreign component read settings: %v", err)
	}
	if _, err := foreign.AccessOwner().UpdateSecuritySettings(t.Context(), first, initial); !errors.Is(err, ErrWorkspaceHandleUnavailable) {
		t.Fatalf("foreign component mutated settings: %v", err)
	}
	if foreign.AccessOwner().SetMCPStarted(first, false) {
		t.Fatal("foreign component controlled MCP")
	}
	if got, err := access.ReadSecuritySettings(t.Context(), first); err != nil || got != want || infrastructureOutboxCount(t, first) != beforeFirst+1 {
		t.Fatalf("foreign attempts changed storage: %#v %v", got, err)
	}
	if infrastructureDurableSettings(t, first) != want || infrastructureDurableSettings(t, second) != otherInitial {
		t.Fatal("foreign attempts changed durable settings behind cache")
	}
	if err := component.WorkspaceOwner().DiscardWorkspace(first, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := access.ReadSecuritySettings(t.Context(), first); !errors.Is(err, ErrWorkspaceHandleUnavailable) {
		t.Fatalf("discarded handle retained settings: %v", err)
	}
	if _, ok := access.MCPTokenSource(first); ok || access.SetMCPStarted(first, true) {
		t.Fatal("discarded handle retained control or token source")
	}
	if got, err := access.ReadSecuritySettings(t.Context(), second); err != nil || got != otherInitial {
		t.Fatalf("discarding first disturbed second: %#v %v", got, err)
	}
}
