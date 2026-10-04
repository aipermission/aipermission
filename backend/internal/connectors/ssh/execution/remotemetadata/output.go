package remotemetadata

import "sync"

type boundedOutput struct {
	mu         sync.Mutex
	data       []byte
	limit      int
	exceeded   bool
	onExceeded func() error
	closeOnce  sync.Once
}

func newBoundedOutput(limit int, onExceeded func() error) *boundedOutput {
	return &boundedOutput{limit: limit, onExceeded: onExceeded}
}

func (output *boundedOutput) Write(value []byte) (int, error) {
	output.mu.Lock()
	remaining := output.limit - len(output.data)
	if remaining > len(value) {
		remaining = len(value)
	}
	if remaining > 0 {
		output.data = append(output.data, value[:remaining]...)
	}
	exceeded := len(value) > remaining
	output.exceeded = output.exceeded || exceeded
	output.mu.Unlock()
	if exceeded {
		output.closeOnce.Do(func() { _ = output.onExceeded() })
	}
	return len(value), nil
}

func (output *boundedOutput) Exceeded() bool {
	output.mu.Lock()
	defer output.mu.Unlock()
	return output.exceeded
}

func (output *boundedOutput) String() string {
	output.mu.Lock()
	defer output.mu.Unlock()
	return string(output.data)
}
