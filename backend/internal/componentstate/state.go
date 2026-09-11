// Package componentstate stores typed, component-owned state without coupling
// workspace lifecycle code to component implementations.
package componentstate

import (
	"errors"
	"reflect"
	"strings"
	"sync"
)

var (
	ErrUnavailable     = errors.New("workspace component state is unavailable")
	ErrInvalidKey      = errors.New("workspace component state key is invalid")
	ErrTypeMismatch    = errors.New("workspace component state type mismatch")
	ErrFactoryRequired = errors.New("workspace component state factory is required")
	ErrNilValue        = errors.New("workspace component state factory returned nil")
)

type Key struct {
	name      string
	valueType reflect.Type
}

func NewKey[T any](name string) Key {
	return Key{name: strings.TrimSpace(name), valueType: reflect.TypeFor[T]()}
}

type Port interface {
	LoadComponentState(Key) (any, bool, error)
	LoadOrCreateComponentState(Key, func() (any, error)) (any, error)
	RegisterComponentLifecycle(Key, Lifecycle) error
	ComponentLifecycles() ([]Lifecycle, error)
}

type slot struct {
	mu        sync.Mutex
	valueType reflect.Type
	value     any
}

type State struct {
	mu             sync.Mutex
	slots          map[string]*slot
	lifecycles     map[string]Lifecycle
	lifecycleOrder []string
}

func New() State {
	return State{slots: make(map[string]*slot), lifecycles: make(map[string]Lifecycle)}
}

func Load[T any](state Port, key Key) (T, bool, error) {
	var zero T
	if state == nil {
		return zero, false, ErrUnavailable
	}
	if key.valueType != reflect.TypeFor[T]() {
		return zero, false, ErrTypeMismatch
	}
	value, ok, err := state.LoadComponentState(key)
	if err != nil || !ok {
		return zero, false, err
	}
	typed, ok := value.(T)
	if !ok || nilState(typed) {
		return zero, false, ErrTypeMismatch
	}
	return typed, true, nil
}

func LoadOrCreate[T any](state Port, key Key, create func() (T, error)) (T, error) {
	var zero T
	if state == nil {
		return zero, ErrUnavailable
	}
	if key.valueType != reflect.TypeFor[T]() {
		return zero, ErrTypeMismatch
	}
	if create == nil {
		return zero, ErrFactoryRequired
	}
	value, err := state.LoadOrCreateComponentState(key, func() (any, error) {
		return create()
	})
	if err != nil {
		return zero, err
	}
	typed, ok := value.(T)
	if !ok || nilState(typed) {
		return zero, ErrTypeMismatch
	}
	return typed, nil
}

func (s *State) LoadComponentState(key Key) (any, bool, error) {
	if s == nil {
		return nil, false, ErrUnavailable
	}
	if err := key.validate(); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	entry := s.slots[key.name]
	s.mu.Unlock()
	if entry == nil {
		return nil, false, nil
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.valueType != key.valueType {
		return nil, false, ErrTypeMismatch
	}
	return entry.value, !nilState(entry.value), nil
}

func (s *State) LoadOrCreateComponentState(key Key, create func() (any, error)) (any, error) {
	entry, err := s.stateSlot(key)
	if err != nil {
		return nil, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.value != nil {
		if nilState(entry.value) {
			return nil, ErrTypeMismatch
		}
		return entry.value, nil
	}
	if create == nil {
		return nil, ErrFactoryRequired
	}
	value, err := create()
	if err != nil {
		return nil, err
	}
	if nilState(value) {
		return nil, ErrNilValue
	}
	if reflect.TypeOf(value) != key.valueType {
		return nil, ErrTypeMismatch
	}
	entry.value = value
	return value, nil
}

func (s *State) stateSlot(key Key) (*slot, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	if err := key.validate(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.slots == nil {
		s.slots = make(map[string]*slot)
	}
	if existing := s.slots[key.name]; existing != nil {
		if existing.valueType != key.valueType {
			return nil, ErrTypeMismatch
		}
		return existing, nil
	}
	created := &slot{valueType: key.valueType}
	s.slots[key.name] = created
	return created, nil
}

func (s *State) RegisterComponentLifecycle(key Key, lifecycle Lifecycle) error {
	if s == nil {
		return ErrUnavailable
	}
	if err := key.validate(); err != nil {
		return err
	}
	if err := lifecycle.validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifecycles == nil {
		s.lifecycles = make(map[string]Lifecycle)
	}
	if _, exists := s.lifecycles[key.name]; !exists {
		s.lifecycleOrder = append(s.lifecycleOrder, key.name)
	}
	s.lifecycles[key.name] = lifecycle
	return nil
}

func (s *State) ComponentLifecycles() ([]Lifecycle, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Lifecycle, 0, len(s.lifecycleOrder))
	for _, name := range s.lifecycleOrder {
		if lifecycle, ok := s.lifecycles[name]; ok {
			result = append(result, lifecycle)
		}
	}
	return result, nil
}

func (key Key) validate() error {
	if key.name == "" || key.valueType == nil {
		return ErrInvalidKey
	}
	return nil
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
