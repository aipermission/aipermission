// Package runtimecontrol owns concurrency-safe mutable gateway runtime state.
package runtimecontrol

import "sync"

// State is scoped to one unlocked database runtime.
type State struct {
	mu         sync.RWMutex
	mcpStarted bool
}

func (s *State) MCPStarted() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mcpStarted
}

func (s *State) SetMCPStarted(enabled bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.mcpStarted = enabled
	s.mu.Unlock()
}
