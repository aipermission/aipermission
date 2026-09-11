package componentstate

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

type stateFixture struct{ id int }
type otherFixture struct{ id int }

func TestStateSeparatesOwnersAndNormalizesKeys(t *testing.T) {
	state := New()
	firstKey := NewKey[*stateFixture](" first ")
	first := &stateFixture{id: 1}
	created, err := LoadOrCreate(&state, firstKey, func() (*stateFixture, error) { return first, nil })
	if err != nil {
		t.Fatal(err)
	}
	if created != first {
		t.Fatalf("created state = %p, want %p", created, first)
	}
	if cached, ok, err := Load[*stateFixture](&state, NewKey[*stateFixture]("first")); err != nil || !ok || cached != first {
		t.Fatalf("cached state = %v, %t, %v; want first state", cached, ok, err)
	}
	secondKey := NewKey[*stateFixture]("second")
	second := &stateFixture{id: 2}
	if _, err := LoadOrCreate(&state, secondKey, func() (*stateFixture, error) { return second, nil }); err != nil {
		t.Fatal(err)
	}
	if cached, ok, err := Load[*stateFixture](&state, secondKey); err != nil || !ok || cached != second {
		t.Fatalf("second state = %v, %t, %v; want second state", cached, ok, err)
	}
}

func TestStateCreatesOnceConcurrentlyAndRetriesFactoryErrors(t *testing.T) {
	state := New()
	key := NewKey[*stateFixture]("retry")
	want := errors.New("factory failed")
	if _, err := LoadOrCreate(&state, key, func() (*stateFixture, error) { return nil, want }); !errors.Is(err, want) {
		t.Fatalf("factory error = %v, want %v", err, want)
	}
	if _, ok, err := Load[*stateFixture](&state, key); err != nil || ok {
		t.Fatalf("failed factory state = %t, %v; want absent", ok, err)
	}

	var calls atomic.Int64
	const workers = 20
	results := make(chan *stateFixture, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			value, err := LoadOrCreate(&state, key, func() (*stateFixture, error) {
				calls.Add(1)
				return &stateFixture{id: 3}, nil
			})
			if err != nil {
				t.Errorf("create component state: %v", err)
				return
			}
			results <- value
		}()
	}
	group.Wait()
	close(results)
	var first *stateFixture
	for result := range results {
		if first == nil {
			first = result
		}
		if result != first {
			t.Fatal("callers observed different component state values")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("factory calls = %d, want 1", calls.Load())
	}
}

func TestStateRejectsInvalidKeysFactoriesAndTypes(t *testing.T) {
	state := New()
	invalid := NewKey[*stateFixture](" ")
	if _, _, err := Load[*stateFixture](&state, invalid); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("blank key error = %v, want ErrInvalidKey", err)
	}
	key := NewKey[*stateFixture]("owner")
	if _, err := LoadOrCreate[*stateFixture](&state, key, nil); !errors.Is(err, ErrFactoryRequired) {
		t.Fatalf("nil factory error = %v, want ErrFactoryRequired", err)
	}
	var typedNil *stateFixture
	if _, err := LoadOrCreate(&state, key, func() (*stateFixture, error) { return typedNil, nil }); !errors.Is(err, ErrNilValue) {
		t.Fatalf("typed-nil error = %v, want ErrNilValue", err)
	}
	if _, err := state.LoadOrCreateComponentState(key, func() (any, error) { return &otherFixture{}, nil }); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("factory type error = %v, want ErrTypeMismatch", err)
	}
	otherKey := NewKey[*otherFixture]("owner")
	if _, err := LoadOrCreate(&state, otherKey, func() (*otherFixture, error) { return &otherFixture{}, nil }); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("duplicate key type error = %v, want ErrTypeMismatch", err)
	}
	if _, _, err := Load[*otherFixture](&state, otherKey); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("load type error = %v, want ErrTypeMismatch", err)
	}
	var nilState *State
	if _, err := LoadOrCreate(nilState, key, func() (*stateFixture, error) { return &stateFixture{}, nil }); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil state error = %v, want ErrUnavailable", err)
	}
}
