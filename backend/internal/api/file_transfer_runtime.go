package api

import (
	"context"
	"sync"
)

type transferControl struct {
	mu      sync.Mutex
	paused  bool
	waiters []chan struct{}
}

func newTransferControl() *transferControl {
	return &transferControl{}
}

func (c *transferControl) Pause() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.paused {
		return false
	}
	c.paused = true
	return true
}

func (c *transferControl) Resume() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.paused {
		return false
	}
	c.paused = false
	for _, waiter := range c.waiters {
		close(waiter)
	}
	c.waiters = nil
	return true
}

func (c *transferControl) Wait(ctx context.Context) error {
	for {
		c.mu.Lock()
		if !c.paused {
			c.mu.Unlock()
			return nil
		}
		waiter := make(chan struct{})
		c.waiters = append(c.waiters, waiter)
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-waiter:
		}
	}
}

func (rt *databaseRuntime) registerTransferCancel(id int64, cancel context.CancelFunc) {
	rt.transferMu.Lock()
	defer rt.transferMu.Unlock()
	if rt.transferCancels == nil {
		rt.transferCancels = map[int64]context.CancelFunc{}
	}
	rt.transferCancels[id] = cancel
}

func (rt *databaseRuntime) registerTransferControl(id int64, control *transferControl) {
	rt.transferMu.Lock()
	defer rt.transferMu.Unlock()
	if rt.transferControls == nil {
		rt.transferControls = map[int64]*transferControl{}
	}
	rt.transferControls[id] = control
}

func (rt *databaseRuntime) unregisterTransferControl(id int64) {
	rt.transferMu.Lock()
	defer rt.transferMu.Unlock()
	delete(rt.transferControls, id)
}

func (rt *databaseRuntime) transferControl(id int64) *transferControl {
	rt.transferMu.Lock()
	defer rt.transferMu.Unlock()
	return rt.transferControls[id]
}

func (rt *databaseRuntime) registerBatchControl(id int64, control *transferControl) {
	rt.transferMu.Lock()
	defer rt.transferMu.Unlock()
	if rt.batchControls == nil {
		rt.batchControls = map[int64]*transferControl{}
	}
	rt.batchControls[id] = control
}

func (rt *databaseRuntime) unregisterBatchControl(id int64) {
	rt.transferMu.Lock()
	defer rt.transferMu.Unlock()
	delete(rt.batchControls, id)
}

func (rt *databaseRuntime) batchControl(id int64) *transferControl {
	rt.transferMu.Lock()
	defer rt.transferMu.Unlock()
	return rt.batchControls[id]
}

func (rt *databaseRuntime) unregisterTransferCancel(id int64) {
	rt.transferMu.Lock()
	defer rt.transferMu.Unlock()
	delete(rt.transferCancels, id)
}

func (rt *databaseRuntime) cancelTransfer(id int64) bool {
	rt.transferMu.Lock()
	cancel := rt.transferCancels[id]
	rt.transferMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (rt *databaseRuntime) registerBatchCancel(id int64, cancel context.CancelFunc) {
	rt.transferMu.Lock()
	defer rt.transferMu.Unlock()
	if rt.batchCancels == nil {
		rt.batchCancels = map[int64]context.CancelFunc{}
	}
	rt.batchCancels[id] = cancel
}

func (rt *databaseRuntime) unregisterBatchCancel(id int64) {
	rt.transferMu.Lock()
	defer rt.transferMu.Unlock()
	delete(rt.batchCancels, id)
}

func (rt *databaseRuntime) cancelBatch(id int64) bool {
	rt.transferMu.Lock()
	cancel := rt.batchCancels[id]
	rt.transferMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}
