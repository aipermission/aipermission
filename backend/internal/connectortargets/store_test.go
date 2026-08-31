package connectortargets

import (
	"context"
	"errors"
	"testing"
)

func TestConnectorTargetRefRoundTrip(t *testing.T) {
	ref := ConnectorTargetRef("postgres", 42, 7)
	if ref != "postgres:42:7" {
		t.Fatalf("ref = %q", ref)
	}
	kind, targetID, profileID, ok := ParseConnectorTargetRef(ref)
	if !ok || kind != "postgres" || targetID != 42 || profileID != 7 {
		t.Fatalf("parse = %q %d %d ok=%v", kind, targetID, profileID, ok)
	}
}

func TestParseConnectorTargetRefRejectsInvalidRefs(t *testing.T) {
	for _, ref := range []string{"", "ssh:1", "postgres", "Postgres:1:2", "postgres:0:2", "postgres:1:0", "postgres:x:2"} {
		if _, _, _, ok := ParseConnectorTargetRef(ref); ok {
			t.Fatalf("expected %q to be rejected", ref)
		}
	}
}

func TestStoreCreatesAndResolvesConnectorTargetProfile(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()

	target, profile := createPostgresTargetProfile(t, ctx, store)
	targets, err := store.ListTargets(ctx, ListTargetsFilter{ConnectorKind: "postgres"})
	if err != nil {
		t.Fatalf("list connector targets: %v", err)
	}
	if len(targets) != 1 || targets[0].ID != target.ID || targets[0].Config["database"] != "app" {
		t.Fatalf("unexpected target list: %#v", targets)
	}
	gotTarget, err := store.GetTarget(ctx, target.ID)
	if err != nil {
		t.Fatalf("get connector target: %v", err)
	}
	if gotTarget.Name != "main-db" || gotTarget.ConnectorKind != "postgres" {
		t.Fatalf("unexpected target: %#v", gotTarget)
	}
	profiles, err := store.ListCredentialProfiles(ctx, target.ID)
	if err != nil {
		t.Fatalf("list connector profiles: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("unexpected profile list: %#v", profiles)
	}
	var listedProfile CredentialProfile
	for _, item := range profiles {
		listedProfile = item
	}
	if listedProfile.ID != profile.ID || listedProfile.EncryptedSecretJSON != "encrypted-secret" {
		t.Fatalf("unexpected profile list: %#v", profiles)
	}
	gotProfile, err := store.GetCredentialProfile(ctx, target.ID, profile.ID)
	if err != nil {
		t.Fatalf("get connector profile: %v", err)
	}
	if gotProfile.EncryptedSecretJSON != "encrypted-secret" || gotProfile.Public["username"] != "app_readonly" {
		t.Fatalf("unexpected profile: %#v", gotProfile)
	}

	resolvedTarget, resolvedProfile, err := store.ResolveConnectorActionTarget(ctx, ConnectorTargetRef("postgres", target.ID, profile.ID))
	if err != nil {
		t.Fatalf("resolve connector target: %v", err)
	}

	if resolvedTarget.Ref != ConnectorTargetRef("postgres", target.ID, profile.ID) {
		t.Fatalf("target ref = %q", resolvedTarget.Ref)
	}
	if resolvedTarget.ConnectorKind != "postgres" || resolvedTarget.Name != "main-db" {
		t.Fatalf("unexpected target: %#v", resolvedTarget)
	}
	if resolvedTarget.ProjectID != target.ProjectID {
		t.Fatalf("resolved target project = %d, want %d", resolvedTarget.ProjectID, target.ProjectID)
	}
	if resolvedTarget.Config["host"] != "10.0.0.15" || resolvedTarget.Config["port"].(float64) != 5432 {
		t.Fatalf("unexpected target config: %#v", resolvedTarget.Config)
	}
	if resolvedProfile.ID != profile.ID || resolvedProfile.TargetID != target.ID {
		t.Fatalf("unexpected profile identity: %#v", resolvedProfile)
	}
	if resolvedProfile.Public["username"] != "app_readonly" {
		t.Fatalf("profile public metadata missing: %#v", resolvedProfile.Public)
	}
	if _, exists := resolvedProfile.Public["password"]; exists {
		t.Fatalf("secret should not be exposed in public metadata: %#v", resolvedProfile.Public)
	}
}

func TestStoreValidatesTransportProjectBoundary(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()

	source, sourceProfile := createPostgresTargetProfile(t, ctx, store)
	transport, err := store.CreateTarget(ctx, CreateTargetInput{
		ProjectID:     source.ProjectID,
		ConnectorKind: "ssh",
		Name:          "same-project-transport",
		Config:        map[string]any{"host": "127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("create transport target: %v", err)
	}
	transportProfile, err := store.CreateCredentialProfile(ctx, CreateCredentialProfileInput{
		TargetID:            transport.ID,
		ConnectorKind:       "ssh",
		Kind:                "private_key",
		Label:               "root",
		Public:              map[string]any{"username": "root"},
		EncryptedSecretJSON: "encrypted-secret",
	})
	if err != nil {
		t.Fatalf("create transport profile: %v", err)
	}
	transportRef := ConnectorTargetRef("ssh", transport.ID, transportProfile.ID)
	if err := store.ValidateTransportTarget(ctx, ConnectorTargetRef("postgres", source.ID, sourceProfile.ID), transportRef); err != nil {
		t.Fatalf("validate same-project transport: %v", err)
	}

	result, err := database.ExecContext(ctx, `INSERT INTO projects (name, slug, status, created_at, updated_at) VALUES ('Other', 'other', 'active', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("create second project: %v", err)
	}
	otherProjectID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("second project id: %v", err)
	}
	otherTarget, err := store.CreateTarget(ctx, CreateTargetInput{
		ProjectID:     otherProjectID,
		ConnectorKind: "ssh",
		Name:          "cross-project-transport",
		Config:        map[string]any{"host": "127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("create cross-project target: %v", err)
	}
	otherProfile, err := store.CreateCredentialProfile(ctx, CreateCredentialProfileInput{
		TargetID:            otherTarget.ID,
		ConnectorKind:       "ssh",
		Kind:                "private_key",
		Label:               "root",
		Public:              map[string]any{"username": "root"},
		EncryptedSecretJSON: "encrypted-secret",
	})
	if err != nil {
		t.Fatalf("create cross-project profile: %v", err)
	}
	err = store.ValidateTransportProject(ctx, source.ProjectID, ConnectorTargetRef("ssh", otherTarget.ID, otherProfile.ID))
	if err == nil || err.Error() != "transport target must belong to the same project" {
		t.Fatalf("cross-project transport error = %v", err)
	}
}

func TestStoreGetTargetReturnsNotFound(t *testing.T) {
	_, err := NewStore(openTargetTestDB(t)).GetTarget(context.Background(), 999)
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("expected ErrTargetNotFound, got %v", err)
	}
}

func TestUpdateTargetRejectsStaleSnapshot(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	target, _ := createPostgresTargetProfile(t, ctx, store)

	updated, err := store.UpdateTarget(ctx, UpdateTargetInput{
		ID: target.ID, ProjectID: target.ProjectID, Name: "main-db-updated",
		Config: target.Config, ExpectedUpdatedAt: target.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("first update: %v", err)
	}
	if updated.UpdatedAt == target.UpdatedAt {
		t.Fatal("expected target revision to change")
	}

	_, err = store.UpdateTarget(ctx, UpdateTargetInput{
		ID: target.ID, ProjectID: target.ProjectID, Name: "stale-overwrite",
		Config: target.Config, ExpectedUpdatedAt: target.UpdatedAt,
	})
	if !errors.Is(err, ErrTargetUpdateConflict) {
		t.Fatalf("stale update error = %v, want ErrTargetUpdateConflict", err)
	}
	current, err := store.GetTarget(ctx, target.ID)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if current.Name != "main-db-updated" {
		t.Fatalf("stale update changed target name to %q", current.Name)
	}
}
