package projectvault

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

func TestResolveSessionValidatesAssignmentAndDrift(t *testing.T) {
	database, store := openTestStore(t)
	ctx := context.Background()
	projects := projectstore.NewStore(database)
	owner, err := projects.Create(ctx, "Owner")
	if err != nil {
		t.Fatalf("owner project: %v", err)
	}
	shared, err := projects.Create(ctx, "Shared")
	if err != nil {
		t.Fatalf("shared project: %v", err)
	}
	item, err := store.Create(ctx, CreateInput{
		Name: "PROJECT_API_KEY", Value: "top-secret-value", OwnerProjectID: owner.ID,
		SharedProjectIDs: []int64{shared.ID}, SecretType: "api_key",
		Source:    "imported",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	resolved, err := store.ResolveSession(ctx, []SessionSelection{{
		ItemID: item.ID, SourceProjectID: shared.ID,
	}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.ContentHash == "" || resolved.Environment.Len() != 1 {
		t.Fatalf("resolution = %#v", resolved)
	}
	snapshot := append([]SessionItem(nil), resolved.Items...)
	resolved.Destroy()
	if _, err := store.ReplaceValue(ctx, ReplaceValueInput{
		ID: item.ID, Value: "new-secret-value", ExpectedValueVersion: item.ValueVersion,
	}); err != nil {
		t.Fatalf("replace value: %v", err)
	}
	if err := store.RevalidateSession(ctx, snapshot); !errors.Is(err, ErrStale) {
		t.Fatalf("revalidate after drift = %v", err)
	}
	if err := store.MarkSessionItemsUsed(ctx, snapshot); !errors.Is(err, ErrStale) {
		t.Fatalf("mark stale session items used = %v", err)
	}
}

func TestResolveSessionRejectsBindingRevisionDrift(t *testing.T) {
	database, store := openTestStore(t)
	ctx := context.Background()
	project, err := projectstore.NewStore(database).Create(ctx, "Binding Drift")
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Create(ctx, CreateInput{
		Name: "BINDING_DRIFT_SECRET", Value: "binding-drift-secret-value",
		OwnerProjectID: project.ID, SecretType: "generic_secret", Source: "imported",
	})
	if err != nil {
		t.Fatal(err)
	}
	targets := connectortargets.NewStore(database)
	target, err := targets.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ConnectorKind: "ssh", Name: "binding-drift-target",
		Config: map[string]any{"host": "127.0.0.1", "port": 22},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := targets.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "ssh", Kind: "private_key", Label: "operator",
		Public: map[string]any{"username": "root"},
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := store.SaveDefaultBinding(ctx, DefaultBindingInput{
		VaultItemID: item.ID, SourceProjectID: project.ID,
		TargetID: target.ID, ProfileID: profile.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	selection := SessionSelection{
		ItemID: item.ID, SourceProjectID: project.ID,
		BindingID: binding.ID, BindingRevision: binding.BindingRevision,
	}
	if _, err := store.SnapshotSession(ctx, []SessionSelection{selection}); err != nil {
		t.Fatalf("snapshot current binding: %v", err)
	}
	if _, err := store.SaveDefaultBinding(ctx, DefaultBindingInput{
		VaultItemID: item.ID, SourceProjectID: project.ID,
		TargetID: target.ID, ProfileID: profile.ID, ReplaceExisting: true,
		ExpectedBindingRevision: binding.BindingRevision,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SnapshotSession(ctx, []SessionSelection{selection}); !errors.Is(err, ErrStale) {
		t.Fatalf("snapshot stale binding = %v", err)
	}
}

func TestResolveSessionRejectsUnassignedProjectAndDuplicateNames(t *testing.T) {
	database, store := openTestStore(t)
	ctx := context.Background()
	projects := projectstore.NewStore(database)
	owner, _ := projects.Create(ctx, "Owner")
	other, _ := projects.Create(ctx, "Other")
	item, err := store.Create(ctx, CreateInput{
		Name: "PROJECT_SECRET", Value: "secret", OwnerProjectID: owner.ID,
		SecretType: "generic_secret", Source: "imported",
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	if _, err := store.ResolveSession(ctx, []SessionSelection{{
		ItemID: item.ID, SourceProjectID: other.ID,
	}}); err == nil {
		t.Fatal("expected unassigned project rejection")
	}
	if _, err := store.ResolveSession(ctx, []SessionSelection{
		{ItemID: item.ID, SourceProjectID: owner.ID},
		{ItemID: item.ID, SourceProjectID: owner.ID},
	}); err == nil {
		t.Fatal("expected duplicate item rejection")
	}
}

func TestResolveSessionRejectsStoredValuesThatAreTooShortForSafeRedaction(t *testing.T) {
	database, store := openTestStore(t)
	ctx := context.Background()
	project, err := projectstore.NewStore(database).Create(ctx, "Short Value")
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Create(ctx, CreateInput{
		Name: "SHORT_SECRET", Value: "short", OwnerProjectID: project.ID,
		SecretType: "generic_secret", Source: "imported",
	})
	if err != nil {
		t.Fatalf("short values remain valid Vault records: %v", err)
	}
	if _, err := store.ResolveSession(ctx, []SessionSelection{{
		ItemID: item.ID, SourceProjectID: project.ID,
	}}); err == nil {
		t.Fatal("short Vault value should not be injectable")
	}
}

func TestSessionItemTrackingFindsLiveSessionsAndDoesNotBlockItemDeletion(t *testing.T) {
	database, store := openTestStore(t)
	ctx := context.Background()
	project, err := projectstore.NewStore(database).Create(ctx, "Tracked Session")
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Create(ctx, CreateInput{
		Name: "TRACKED_SESSION_SECRET", Value: "tracked-session-secret",
		OwnerProjectID: project.ID, SecretType: "generic_secret", Source: "imported",
	})
	if err != nil {
		t.Fatal(err)
	}
	targets := connectortargets.NewStore(database)
	target, err := targets.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ConnectorKind: "ssh", Name: "tracked-target",
		Config: map[string]any{"host": "127.0.0.1", "port": 22},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := targets.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "ssh", Kind: "private_key", Label: "operator",
		Public: map[string]any{"username": "root"},
	})
	if err != nil {
		t.Fatal(err)
	}
	surface, err := targets.EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: "ssh", TargetID: target.ID, ProfileID: profile.ID,
		CapabilityKind: connectortargets.RuntimeCapabilityLiveConsole,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := database.ExecContext(ctx, `
		INSERT INTO console_sessions (
			runtime_id, generation, name, status, created_at, updated_at
		) VALUES (?, 1, 'tracked session', 'connected', ?, ?)`,
		surface.ID, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, _ := result.LastInsertId()
	tracked := []SessionItem{{
		ItemID: item.ID, Name: item.Name, SourceProjectID: project.ID,
		ValueVersion: item.ValueVersion, MetadataRevision: item.MetadataRevision,
	}}
	if err := store.RecordSessionItems(ctx, sessionID, tracked); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ActiveSessionsForMutation(ctx, SessionMutationScope{ItemID: item.ID})
	if err != nil || len(sessions) != 1 || sessions[0].SessionID != sessionID {
		t.Fatalf("active sessions = %#v, %v", sessions, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE console_sessions SET status = 'closed' WHERE id = ?`, sessionID); err != nil {
		t.Fatal(err)
	}
	sessions, err = store.ActiveSessionsForMutation(ctx, SessionMutationScope{ItemID: item.ID})
	if err != nil || len(sessions) != 0 {
		t.Fatalf("closed sessions = %#v, %v", sessions, err)
	}
	if err := store.Delete(ctx, item.ID, item.ValueVersion, item.MetadataRevision); err != nil {
		t.Fatalf("delete tracked item: %v", err)
	}
}
