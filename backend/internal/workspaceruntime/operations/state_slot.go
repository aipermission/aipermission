package operations

import (
	"errors"
	"reflect"
	"sync"
)

var ErrStateTypeMismatch = errors.New("operation state type mismatch")

// StateSlot stores component-owned state without coupling the workspace to the
// component's concrete implementation.
type StateSlot struct {
	mu    sync.Mutex
	value any
}

func LoadState[T any](slot *StateSlot) (T, bool) {
	var zero T
	if slot == nil {
		return zero, false
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	value, ok := slot.value.(T)
	return value, ok && !nilState(value)
}

func LoadOrCreateState[T any](slot *StateSlot, create func() (T, error)) (T, error) {
	var zero T
	if slot == nil {
		return zero, errors.New("operation state slot is required")
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.value != nil {
		value, ok := slot.value.(T)
		if !ok || nilState(value) {
			return zero, ErrStateTypeMismatch
		}
		return value, nil
	}
	if create == nil {
		return zero, errors.New("operation state factory is required")
	}
	value, err := create()
	if err != nil {
		return zero, err
	}
	if nilState(value) {
		return zero, errors.New("operation state factory returned nil")
	}
	slot.value = value
	return value, nil
}

func nilState(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
