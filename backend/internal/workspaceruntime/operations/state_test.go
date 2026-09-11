package operations

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

type stateFixture struct{ id int }

func TestLoadOrCreateStatePublishesOneValue(t *testing.T) {
	slot := &StateSlot{}
	var calls atomic.Int64
	const workers = 20
	results := make(chan *stateFixture, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			owner, err := LoadOrCreateState(slot, func() (*stateFixture, error) {
				calls.Add(1)
				return &stateFixture{id: 1}, nil
			})
			if err != nil {
				t.Errorf("create state: %v", err)
				return
			}
			results <- owner
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
			t.Fatal("callers observed different state values")
		}
	}
	cached, ok := LoadState[*stateFixture](slot)
	if calls.Load() != 1 || !ok || cached != first {
		t.Fatalf("factory calls=%d cached=%p want=%p", calls.Load(), cached, first)
	}
}

func TestLoadOrCreateStateRejectsInvalidFactoriesAndTypeDrift(t *testing.T) {
	slot := &StateSlot{}
	if _, err := LoadOrCreateState[*stateFixture](nil, func() (*stateFixture, error) { return &stateFixture{}, nil }); err == nil {
		t.Fatal("nil slot unexpectedly accepted")
	}
	if _, err := LoadOrCreateState[*stateFixture](slot, nil); err == nil {
		t.Fatal("nil factory unexpectedly accepted")
	}
	if _, err := LoadOrCreateState(slot, func() (*stateFixture, error) { return nil, nil }); err == nil {
		t.Fatal("typed-nil state unexpectedly accepted")
	}
	want := errors.New("factory failed")
	if _, err := LoadOrCreateState(slot, func() (*stateFixture, error) { return nil, want }); !errors.Is(err, want) {
		t.Fatalf("factory error = %v, want %v", err, want)
	}
	stored, err := LoadOrCreateState(slot, func() (*stateFixture, error) { return &stateFixture{id: 2}, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateState(slot, func() (string, error) { return "wrong", nil }); !errors.Is(err, ErrStateTypeMismatch) {
		t.Fatalf("type drift error = %v, want ErrStateTypeMismatch", err)
	}
	if current, ok := LoadState[*stateFixture](slot); !ok || current != stored {
		t.Fatal("type drift changed cached state")
	}
}
