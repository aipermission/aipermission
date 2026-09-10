package projectvault

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

type runtimeTestDelivery struct {
	deliveryCalls  int
	exclusiveCalls int
	err            error
}

func (d *runtimeTestDelivery) AcquireDelivery(context.Context) (func(), error) {
	d.deliveryCalls++
	if d.err != nil {
		return nil, d.err
	}
	return func() {}, nil
}

func (d *runtimeTestDelivery) AcquireExclusive(context.Context) (func(), error) {
	d.exclusiveCalls++
	if d.err != nil {
		return nil, d.err
	}
	return func() {}, nil
}

type runtimeTestMutations struct {
	database *sql.DB
	actions  []string
}

func (m *runtimeTestMutations) WithMutation(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
	tx, err := m.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := mutate(tx); err != nil {
		return err
	}
	_ = payload()
	if err := tx.Commit(); err != nil {
		return err
	}
	m.actions = append(m.actions, action)
	return nil
}

func (m *runtimeTestMutations) Observe(_ context.Context, action string, payload any) error {
	if payload == nil {
		return errors.New("audit payload is required")
	}
	m.actions = append(m.actions, action)
	return nil
}

type runtimeTestHarness struct {
	runtime     *Runtime
	delivery    *runtimeTestDelivery
	mutations   *runtimeTestMutations
	projectID   int64
	invalidated int
}

func newRuntimeTestHarness(t *testing.T) *runtimeTestHarness {
	t.Helper()
	database, store := openTestStore(t)
	project, err := projectstore.NewStore(database).Create(t.Context(), "Runtime Owner")
	if err != nil {
		t.Fatal(err)
	}
	harness := &runtimeTestHarness{
		delivery:  &runtimeTestDelivery{},
		mutations: &runtimeTestMutations{database: database},
		projectID: project.ID,
	}
	runtime, err := NewRuntime(RuntimeDependencies{
		Store: store, Delivery: harness.delivery, Mutations: harness.mutations,
		InvalidateSessions: func(context.Context, []SessionReference, SessionMutationScope) error {
			harness.invalidated++
			return nil
		},
		AllowGenerate: func(string) bool { return true },
		AllowReveal:   func(string) bool { return true },
		Nonce:         func() (string, error) { return "nonce-one", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.runtime = runtime
	return harness
}

func (h *runtimeTestHarness) create(t *testing.T) Item {
	t.Helper()
	item, err := h.runtime.Create(t.Context(), CreateInput{
		Name: "RUNTIME_SECRET", Value: "runtime-secret-value", OwnerProjectID: h.projectID,
		SecretType: "generic_secret", Source: "imported",
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestRuntimeCreateAndRevealFenceSecretsAndRequireAudit(t *testing.T) {
	harness := newRuntimeTestHarness(t)
	item := harness.create(t)
	value, err := harness.runtime.Reveal(t.Context(), item.ID, "reveal-key")
	if err != nil {
		t.Fatal(err)
	}
	if value != "runtime-secret-value" {
		t.Fatalf("Reveal() = %q", value)
	}
	if harness.delivery.exclusiveCalls != 1 || harness.delivery.deliveryCalls != 1 {
		t.Fatalf("delivery calls = %d exclusive calls = %d", harness.delivery.deliveryCalls, harness.delivery.exclusiveCalls)
	}
	want := []string{"vault.item.created", "vault.item.revealed"}
	if len(harness.mutations.actions) != len(want) {
		t.Fatalf("audit actions = %#v", harness.mutations.actions)
	}
	for index := range want {
		if harness.mutations.actions[index] != want[index] {
			t.Fatalf("audit actions = %#v", harness.mutations.actions)
		}
	}
}

func TestRuntimeGeneratedPreviewIsSingleCurrentVersion(t *testing.T) {
	harness := newRuntimeTestHarness(t)
	item := harness.create(t)
	first, err := harness.runtime.GeneratePreview(t.Context(), item.ID, "hex_secret", "preview-key")
	if err != nil {
		t.Fatal(err)
	}
	harness.runtime.nonce = func() (string, error) { return "nonce-two", nil }
	second, err := harness.runtime.GeneratePreview(t.Context(), item.ID, "hex_secret", "preview-key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.runtime.ReplaceValue(t.Context(), ReplaceRuntimeValueInput{
		ID: item.ID, Source: "generated", GeneratorKind: "hex_secret",
		PreviewToken: first.PreviewToken, ExpectedValueVersion: item.ValueVersion,
	})
	var validation ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("superseded preview error = %v", err)
	}
	replaced, err := harness.runtime.ReplaceValue(t.Context(), ReplaceRuntimeValueInput{
		ID: item.ID, Source: "generated", GeneratorKind: "hex_secret",
		PreviewToken: second.PreviewToken, ExpectedValueVersion: item.ValueVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := harness.runtime.store.Reveal(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if value != second.Value || replaced.ValueVersion != item.ValueVersion+1 || harness.invalidated != 1 {
		t.Fatalf("replacement = %#v value_match=%v invalidations=%d", replaced, value == second.Value, harness.invalidated)
	}
}

func TestRuntimeFailsBeforeSecretAccessWhenDeliveryFenceRejects(t *testing.T) {
	harness := newRuntimeTestHarness(t)
	want := errors.New("workspace locking")
	harness.delivery.err = want
	_, err := harness.runtime.Create(t.Context(), CreateInput{})
	if !errors.Is(err, want) {
		t.Fatalf("Create() error = %v", err)
	}
	_, err = harness.runtime.Reveal(t.Context(), 1, "reveal-key")
	if !errors.Is(err, want) {
		t.Fatalf("Reveal() error = %v", err)
	}
}
