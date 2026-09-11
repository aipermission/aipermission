package runtimeindex

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestIndexCreatesOneTypedValuePerRuntime(t *testing.T) {
	var index Index[*int]
	var calls atomic.Int64
	var wait sync.WaitGroup
	values := make(chan *int, 16)
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, err := index.LoadOrCreate("runtime-1", func() (*int, error) {
				calls.Add(1)
				created := 7
				return &created, nil
			})
			if err != nil {
				t.Errorf("load or create: %v", err)
				return
			}
			values <- value
		}()
	}
	wait.Wait()
	close(values)
	var first *int
	for value := range values {
		if first == nil {
			first = value
		}
		if value != first {
			t.Fatal("concurrent callers received different values")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("factory calls = %d", calls.Load())
	}
	if removed, ok := index.Delete("runtime-1"); !ok || removed != first {
		t.Fatal("delete did not return the owned value")
	}
	if _, ok := index.Load("runtime-1"); ok {
		t.Fatal("deleted value remained visible")
	}
}

func TestIndexFailsClosedForInvalidConstruction(t *testing.T) {
	var index Index[int]
	if _, err := index.LoadOrCreate("", func() (int, error) { return 1, nil }); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("invalid id error = %v", err)
	}
	if _, err := index.LoadOrCreate("runtime", nil); !errors.Is(err, ErrFactoryRequired) {
		t.Fatalf("missing factory error = %v", err)
	}
	want := errors.New("failed")
	if _, err := index.LoadOrCreate("runtime", func() (int, error) { return 0, want }); !errors.Is(err, want) {
		t.Fatalf("factory error = %v", err)
	}
	if _, ok := index.Load("runtime"); ok {
		t.Fatal("failed construction was cached")
	}
}
