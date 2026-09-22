package operationcoord

import (
	"context"
	"sync"
)

type entry struct {
	token chan struct{}
	refs  int
}

// Coordinator serializes work per key while allowing unrelated keys to run in parallel.
type Coordinator[K comparable] struct {
	mu      sync.Mutex
	entries map[K]*entry
}

func (c *Coordinator[K]) Acquire(ctx context.Context, key K) (func(), error) {
	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[K]*entry)
	}
	item := c.entries[key]
	if item == nil {
		item = &entry{token: make(chan struct{}, 1)}
		item.token <- struct{}{}
		c.entries[key] = item
	}
	item.refs++
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		c.releaseReference(key, item)
		return nil, ctx.Err()
	case <-item.token:
	}
	if err := ctx.Err(); err != nil {
		item.token <- struct{}{}
		c.releaseReference(key, item)
		return nil, err
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			item.token <- struct{}{}
			c.releaseReference(key, item)
		})
	}, nil
}

func (c *Coordinator[K]) releaseReference(key K, item *entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item.refs--
	if item.refs == 0 && c.entries[key] == item {
		delete(c.entries, key)
	}
}
