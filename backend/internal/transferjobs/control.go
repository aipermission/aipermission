// Package transferjobs owns in-memory transfer cancellation and pause state.
// It has no connector, credential, database, or HTTP dependencies.
package transferjobs

import (
	"context"
	"sync"
)

// Control is a reusable pause gate. Its zero value is ready to run.
type Control struct {
	mu     sync.Mutex
	resume chan struct{}
}

func (c *Control) Pause() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.resume != nil {
		return false
	}
	c.resume = make(chan struct{})
	return true
}

func (c *Control) Resume() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.resume == nil {
		return false
	}
	close(c.resume)
	c.resume = nil
	return true
}

func (c *Control) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.mu.Lock()
		resume := c.resume
		c.mu.Unlock()
		if resume == nil {
			return nil
		}
		// One broadcast channel per pause cycle, not one retained entry per waiter.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-resume:
		}
	}
}
