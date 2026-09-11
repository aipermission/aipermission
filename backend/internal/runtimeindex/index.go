// Package runtimeindex provides a small typed index for owner-managed workspace state.
package runtimeindex

import (
	"errors"
	"strings"
	"sync"
)

var (
	ErrInvalidID       = errors.New("runtime identity is required")
	ErrFactoryRequired = errors.New("runtime factory is required")
)

type Index[T any] struct {
	mu     sync.Mutex
	values map[string]T
}

func (index *Index[T]) Load(id string) (T, bool) {
	var zero T
	id = strings.TrimSpace(id)
	if index == nil || id == "" {
		return zero, false
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	value, ok := index.values[id]
	return value, ok
}

func (index *Index[T]) LoadOrCreate(id string, create func() (T, error)) (T, error) {
	var zero T
	id = strings.TrimSpace(id)
	if index == nil || id == "" {
		return zero, ErrInvalidID
	}
	if create == nil {
		return zero, ErrFactoryRequired
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	if value, ok := index.values[id]; ok {
		return value, nil
	}
	value, err := create()
	if err != nil {
		return zero, err
	}
	if index.values == nil {
		index.values = make(map[string]T)
	}
	index.values[id] = value
	return value, nil
}

func (index *Index[T]) Delete(id string) (T, bool) {
	var zero T
	id = strings.TrimSpace(id)
	if index == nil || id == "" {
		return zero, false
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	value, ok := index.values[id]
	if ok {
		delete(index.values, id)
	}
	return value, ok
}
