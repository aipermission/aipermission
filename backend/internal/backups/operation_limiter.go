package backups

import (
	"context"
	"sync"
)

const defaultConcurrentOperations = 2

type OperationLimiter struct {
	once  sync.Once
	slots chan struct{}
}

func (limiter *OperationLimiter) Acquire(ctx context.Context) (func(), error) {
	limiter.once.Do(func() {
		limiter.slots = make(chan struct{}, defaultConcurrentOperations)
	})
	select {
	case limiter.slots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-limiter.slots }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
