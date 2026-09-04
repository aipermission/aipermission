package api

import (
	"context"
	"errors"
	"sync"
)

// vaultDeliveryCoordinator lets independent secret deliveries proceed together
// while credential, permission, and trust mutations wait for a quiescent point.
type vaultDeliveryCoordinator struct {
	mu             sync.Mutex
	readers        int
	writer         bool
	waitingWriters int
	changed        chan struct{}
}

func (c *vaultDeliveryCoordinator) acquireDelivery(ctx context.Context) (func(), error) {
	if c == nil {
		return nil, errors.New("Vault delivery coordinator is not configured")
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.initializeLocked()
		if !c.writer && c.waitingWriters == 0 {
			c.readers++
			c.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					c.mu.Lock()
					c.readers--
					c.notifyLocked()
					c.mu.Unlock()
				})
			}, nil
		}
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

func (c *vaultDeliveryCoordinator) acquireExclusive(ctx context.Context) (func(), error) {
	if c == nil {
		return nil, errors.New("Vault delivery coordinator is not configured")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.initializeLocked()
	c.waitingWriters++
	c.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			c.cancelExclusiveWait()
			return nil, err
		}
		c.mu.Lock()
		if !c.writer && c.readers == 0 {
			c.waitingWriters--
			c.writer = true
			c.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					c.mu.Lock()
					c.writer = false
					c.notifyLocked()
					c.mu.Unlock()
				})
			}, nil
		}
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			c.cancelExclusiveWait()
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

func (c *vaultDeliveryCoordinator) cancelExclusiveWait() {
	c.mu.Lock()
	c.waitingWriters--
	c.notifyLocked()
	c.mu.Unlock()
}

func (c *vaultDeliveryCoordinator) initializeLocked() {
	if c.changed == nil {
		c.changed = make(chan struct{})
	}
}

func (c *vaultDeliveryCoordinator) notifyLocked() {
	c.initializeLocked()
	close(c.changed)
	c.changed = make(chan struct{})
}
