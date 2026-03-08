package testdata

import (
	"context"
	"sync"
)

// Clean code — correct concurrency patterns. Should produce zero findings.

type SafeCache struct {
	mu    sync.RWMutex
	items map[string]string
	done  chan struct{}
	once  sync.Once
}

func NewSafeCache() *SafeCache {
	return &SafeCache{
		items: make(map[string]string),
		done:  make(chan struct{}),
	}
}

func (c *SafeCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.items[key]
	return v, ok
}

func (c *SafeCache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = value
}

func (c *SafeCache) Close() {
	c.once.Do(func() {
		close(c.done)
	})
}

// Worker with proper context handling and shutdown coordination.
func (c *SafeCache) Watch(ctx context.Context, updates <-chan string) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case val, ok := <-updates:
			if !ok {
				return
			}
			c.Set("latest", val)
		}
	}
}

// Goroutine with proper coordination via WaitGroup.
func RunWorkers(ctx context.Context, n int, work func(context.Context)) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1) // Add BEFORE launching goroutine
		go func() {
			defer wg.Done()
			work(ctx)
		}()
	}
	wg.Wait()
}
