package gatewayconnectormanagement

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestLifecycleDeleteTargetOwnsAuditPayloadWithoutMutatingCaller(t *testing.T) {
	original := map[string]any{"source": "test"}
	var actor, action string
	var payload map[string]any
	service := NewLifecycleService(LifecycleServiceDependencies{
		Mutate: func(_ context.Context, gotActor, gotAction string, build func() any, _ func(*sql.Tx) error) error {
			actor, action = gotActor, gotAction
			payload = build().(map[string]any)
			return nil
		},
		Redact:          func(_ context.Context, value string) string { return value },
		InvalidateVault: func(context.Context, int64, int64, string) error { return nil },
	})
	target := connectortargets.Target{ID: 9, ConnectorKind: "fixture", Name: "primary"}

	if err := service.DeleteTarget(t.Context(), target, original); err != nil {
		t.Fatal(err)
	}
	if actor != "user" || action != "connector.target.deleted" {
		t.Fatalf("audit identity = %q %q", actor, action)
	}
	want := map[string]any{"source": "test", "target_id": int64(9), "connector_kind": "fixture", "name": "primary"}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("payload = %#v, want %#v", payload, want)
	}
	if !reflect.DeepEqual(original, map[string]any{"source": "test"}) {
		t.Fatalf("caller payload mutated: %#v", original)
	}
}

func TestLifecycleCredentialChangeInvalidatesVaultBeforeRequests(t *testing.T) {
	steps := []string{}
	service := NewLifecycleService(LifecycleServiceDependencies{
		Mutate: func(_ context.Context, _, _ string, _ func() any, _ func(*sql.Tx) error) error {
			steps = append(steps, "requests")
			return nil
		},
		Redact: func(_ context.Context, value string) string { return value },
		InvalidateVault: func(_ context.Context, targetID, profileID int64, reason string) error {
			if targetID != 4 || profileID != 7 || reason != "vault stale" {
				t.Fatalf("vault invalidation = %d %d %q", targetID, profileID, reason)
			}
			steps = append(steps, "vault")
			return nil
		},
	})

	err := service.AfterCredentialChange(t.Context(), connectormanagement.TargetLifecycleChange{
		TargetID: 4, ProfileID: 7, StaleReason: "vault stale", UserMessage: "request stale",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(steps, []string{"vault", "requests"}) {
		t.Fatalf("lifecycle order = %#v", steps)
	}
}

func TestLifecycleStopsWhenVaultInvalidationFails(t *testing.T) {
	want := errors.New("vault unavailable")
	mutated := false
	service := NewLifecycleService(LifecycleServiceDependencies{
		Mutate: func(context.Context, string, string, func() any, func(*sql.Tx) error) error {
			mutated = true
			return nil
		},
		Redact:          func(_ context.Context, value string) string { return value },
		InvalidateVault: func(context.Context, int64, int64, string) error { return want },
	})

	err := service.AfterCredentialChange(t.Context(), connectormanagement.TargetLifecycleChange{TargetID: 2})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if mutated {
		t.Fatal("request mutation ran after Vault invalidation failed")
	}
}

func TestLifecycleRejectsIncompleteDependencies(t *testing.T) {
	err := NewLifecycleService(LifecycleServiceDependencies{}).DeleteTarget(
		t.Context(), connectortargets.Target{ID: 1}, nil,
	)
	if !errors.Is(err, ErrLifecycleUnavailable) {
		t.Fatalf("error = %v, want %v", err, ErrLifecycleUnavailable)
	}
}
