package vaultsessions

import (
	"context"
	"errors"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

// DeliveryCoordinator allows secret deliveries to proceed together while
// credential, permission, and trust mutations wait for a quiescent point.
type DeliveryCoordinator struct {
	mu             sync.Mutex
	readers        int
	writer         bool
	waitingWriters int
	changed        chan struct{}
	identity       connectors.DeliveryAdmissionIdentity
	deliveryGuard  func(context.Context) error
}

func (c *DeliveryCoordinator) AdmissionIdentity() *connectors.DeliveryAdmissionIdentity {
	if c == nil {
		return nil
	}
	return &c.identity
}

func (c *DeliveryCoordinator) SetDeliveryGuard(guard func(context.Context) error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.deliveryGuard = guard
	c.mu.Unlock()
}

func (c *DeliveryCoordinator) AcquireDelivery(ctx context.Context) (func(), error) {
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
			guard := c.deliveryGuard
			c.mu.Unlock()
			var once sync.Once
			release := func() {
				once.Do(func() {
					c.mu.Lock()
					c.readers--
					c.notifyLocked()
					c.mu.Unlock()
				})
			}
			if guard != nil {
				if err := guard(ctx); err != nil {
					release()
					return nil, err
				}
			}
			return release, nil
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

func (c *DeliveryCoordinator) AcquireExclusive(ctx context.Context) (func(), error) {
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

func (c *DeliveryCoordinator) cancelExclusiveWait() {
	c.mu.Lock()
	c.waitingWriters--
	c.notifyLocked()
	c.mu.Unlock()
}

func (c *DeliveryCoordinator) initializeLocked() {
	if c.changed == nil {
		c.changed = make(chan struct{})
	}
}

func (c *DeliveryCoordinator) notifyLocked() {
	c.initializeLocked()
	close(c.changed)
	c.changed = make(chan struct{})
}
