package gatewayinfrastructure

import (
	"context"
	"sync"
)

const defaultConcurrentBackupOperations = 2

type backupOperationLimiter struct {
	once  sync.Once
	slots chan struct{}
}

func (limiter *backupOperationLimiter) acquire(ctx context.Context) (func(), error) {
	limiter.once.Do(func() { limiter.slots = make(chan struct{}, defaultConcurrentBackupOperations) })
	select {
	case limiter.slots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-limiter.slots }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
