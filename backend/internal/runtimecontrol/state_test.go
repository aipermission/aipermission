package runtimecontrol

import (
	"sync"
	"testing"
)

func TestStateMCPAvailability(t *testing.T) {
	var state State
	if state.MCPStarted() {
		t.Fatal("zero state must be stopped")
	}
	state.SetMCPStarted(true)
	if !state.MCPStarted() {
		t.Fatal("state did not start")
	}
	state.SetMCPStarted(false)
	if state.MCPStarted() {
		t.Fatal("state did not stop")
	}
}

func TestStateMCPAvailabilityIsConcurrent(t *testing.T) {
	var state State
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func(value bool) {
			defer workers.Done()
			for attempt := 0; attempt < 100; attempt++ {
				state.SetMCPStarted(value)
				_ = state.MCPStarted()
			}
		}(i%2 == 0)
	}
	workers.Wait()
}
