package componentstate

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestComponentLifecyclesCloseWaitAndAbortInReverseRegistrationOrder(t *testing.T) {
	state := New()
	firstKey := NewKey[*stateFixture]("first")
	secondKey := NewKey[*stateFixture]("second")
	var closed, waited, aborted []string
	register := func(key Key, name string, drained bool, closeErr error) {
		t.Helper()
		err := RegisterLifecycle(&state, key, Lifecycle{
			Name: name,
			Close: func(context.Context) (bool, error) {
				closed = append(closed, name)
				return drained, closeErr
			},
			Abort: func() { aborted = append(aborted, name) },
			Wait: func(context.Context) bool {
				waited = append(waited, name)
				return drained
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	firstErr := errors.New("first close failed")
	register(firstKey, "first", true, firstErr)
	register(secondKey, "second", false, nil)

	report := CloseComponents(t.Context(), &state)
	if report.Registered != 2 || report.Drained || !errors.Is(report.Err, firstErr) {
		t.Fatalf("close report = %#v", report)
	}
	if !reflect.DeepEqual(closed, []string{"second", "first"}) {
		t.Fatalf("close order = %v", closed)
	}
	if WaitComponents(t.Context(), &state) {
		t.Fatal("pending component reported drained")
	}
	if !reflect.DeepEqual(waited, []string{"second", "first"}) {
		t.Fatalf("wait order = %v", waited)
	}
	if err := AbortComponents(&state); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(aborted, []string{"second", "first"}) {
		t.Fatalf("abort order = %v", aborted)
	}
}

func TestComponentLifecycleRegistrationIsIdempotentByStateKey(t *testing.T) {
	state := New()
	key := NewKey[*stateFixture]("component")
	lifecycle := func(name string) Lifecycle {
		return Lifecycle{
			Name:  name,
			Close: func(context.Context) (bool, error) { return true, nil },
			Abort: func() {}, Wait: func(context.Context) bool { return true },
		}
	}
	if err := RegisterLifecycle(&state, key, lifecycle("first")); err != nil {
		t.Fatal(err)
	}
	if err := RegisterLifecycle(&state, key, lifecycle("replacement")); err != nil {
		t.Fatal(err)
	}
	report := CloseComponents(t.Context(), &state)
	if report.Registered != 1 || !report.Drained || report.Err != nil {
		t.Fatalf("close report = %#v", report)
	}
	registered, err := state.ComponentLifecycles()
	if err != nil || len(registered) != 1 || registered[0].Name != "replacement" {
		t.Fatalf("registered lifecycles = %#v, %v", registered, err)
	}
}

func TestComponentLifecycleFailsClosedForInvalidStateOrHooks(t *testing.T) {
	key := NewKey[*stateFixture]("component")
	if err := RegisterLifecycle(nil, key, Lifecycle{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil state registration error = %v", err)
	}
	state := New()
	if err := RegisterLifecycle(&state, key, Lifecycle{Name: "incomplete"}); !errors.Is(err, ErrInvalidLifecycle) {
		t.Fatalf("incomplete lifecycle error = %v", err)
	}
	report := CloseComponents(t.Context(), nil)
	if !report.Drained || !errors.Is(report.Err, ErrUnavailable) {
		t.Fatalf("nil state close report = %#v", report)
	}
}
