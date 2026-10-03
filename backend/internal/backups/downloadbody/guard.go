// Package downloadbody bounds a remote transfer's total and no-progress time.
package downloadbody

import (
	"context"
	"io"
	"sync"
	"time"
)

type Guard struct {
	context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	timer    *time.Timer
	idle     time.Duration
	deadline time.Time
	finished bool
}

func New(parent context.Context, total, idle time.Duration) *Guard {
	ctx, cancel := context.WithTimeout(parent, total)
	guard := &Guard{Context: ctx, cancel: cancel, idle: idle, deadline: time.Now().Add(idle)}
	guard.mu.Lock()
	guard.timer = time.AfterFunc(idle, guard.expire)
	guard.mu.Unlock()
	return guard
}

func (guard *Guard) expire() {
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if guard.finished {
		return
	}
	if remaining := time.Until(guard.deadline); remaining > 0 {
		guard.timer.Reset(remaining)
		return
	}
	guard.finished = true
	guard.cancel()
}

func (guard *Guard) Close() {
	guard.mu.Lock()
	guard.finished = true
	guard.timer.Stop()
	guard.cancel()
	guard.mu.Unlock()
}

func (guard *Guard) Reader(body io.Reader) io.Reader {
	return progressReader{guard: guard, body: body}
}

type progressReader struct {
	guard *Guard
	body  io.Reader
}

func (reader progressReader) Read(buffer []byte) (int, error) {
	if err := reader.guard.Err(); err != nil {
		return 0, err
	}
	n, err := reader.body.Read(buffer)
	reader.guard.mu.Lock()
	if !reader.guard.finished {
		if !time.Now().Before(reader.guard.deadline) {
			reader.guard.finished = true
			reader.guard.cancel()
		} else if err == io.EOF {
			reader.guard.timer.Stop()
			reader.guard.finished = true
		} else if n > 0 {
			reader.guard.deadline = time.Now().Add(reader.guard.idle)
			reader.guard.timer.Reset(reader.guard.idle)
		}
	}
	reader.guard.mu.Unlock()
	if cause := reader.guard.Err(); cause != nil {
		return n, cause
	}
	return n, err
}
