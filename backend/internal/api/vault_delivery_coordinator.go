package api

import (
	"context"
	"errors"
	"sync"
)

type vaultDeliveryCoordinator struct {
	once sync.Once
	gate chan struct{}
}

func (c *vaultDeliveryCoordinator) acquire(ctx context.Context) (func(), error) {
	if c == nil {
		return nil, errors.New("Vault delivery coordinator is not configured")
	}
	c.once.Do(func() {
		c.gate = make(chan struct{}, 1)
		c.gate <- struct{}{}
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.gate:
		var once sync.Once
		return func() {
			once.Do(func() { c.gate <- struct{}{} })
		}, nil
	}
}
