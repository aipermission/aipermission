package componentstate

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidLifecycle = errors.New("workspace component lifecycle is invalid")

type Lifecycle struct {
	Name  string
	Close func(context.Context) (bool, error)
	Abort func()
	Wait  func(context.Context) bool
}

type CloseReport struct {
	Registered int
	Drained    bool
	Err        error
}

func RegisterLifecycle(state Port, key Key, lifecycle Lifecycle) error {
	if state == nil {
		return ErrUnavailable
	}
	return state.RegisterComponentLifecycle(key, lifecycle)
}

func CloseComponents(ctx context.Context, state Port) CloseReport {
	lifecycles, err := componentLifecycles(state)
	if err != nil {
		return CloseReport{Drained: true, Err: err}
	}
	report := CloseReport{Registered: len(lifecycles), Drained: true}
	var failures []error
	for index := len(lifecycles) - 1; index >= 0; index-- {
		lifecycle := lifecycles[index]
		drained, closeErr := lifecycle.Close(ctx)
		if !drained {
			report.Drained = false
		}
		if closeErr != nil {
			failures = append(failures, fmt.Errorf("close workspace component %q: %w", lifecycle.Name, closeErr))
		}
	}
	report.Err = errors.Join(failures...)
	return report
}

func AbortComponents(state Port) error {
	lifecycles, err := componentLifecycles(state)
	if err != nil {
		return err
	}
	for index := len(lifecycles) - 1; index >= 0; index-- {
		lifecycles[index].Abort()
	}
	return nil
}

func WaitComponents(ctx context.Context, state Port) bool {
	lifecycles, err := componentLifecycles(state)
	if err != nil {
		return true
	}
	drained := true
	for index := len(lifecycles) - 1; index >= 0; index-- {
		if !lifecycles[index].Wait(ctx) {
			drained = false
		}
	}
	return drained
}

func componentLifecycles(state Port) ([]Lifecycle, error) {
	if state == nil {
		return nil, ErrUnavailable
	}
	return state.ComponentLifecycles()
}

func (lifecycle Lifecycle) validate() error {
	if strings.TrimSpace(lifecycle.Name) == "" || lifecycle.Close == nil || lifecycle.Abort == nil || lifecycle.Wait == nil {
		return ErrInvalidLifecycle
	}
	return nil
}
