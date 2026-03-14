// Package buggy_app is a deliberately broken HTTP cache service used as a
// golden fixture for concurrency-lens checks. Every bug is intentional.
package main

import (
	"fmt"
	"sync"
)

// --- BUG: check-maps ---
// cache is a package-level map written from multiple goroutines without a lock.
var cache = make(map[string]string)

// CacheWriter spawns goroutines that write to the shared map without any mutex.
func CacheWriter(keys []string) {
	for _, k := range keys {
		// BUG(check-closure): captures loop variable k without passing as arg (pre-1.22)
		go func() {
			cache[k] = "value" // BUG(check-maps): unprotected concurrent map write
		}()
	}
}

// --- BUG: check-ownership ---
// Counter is a struct whose fields are documented as "protected by mu"
// but are in fact read outside the lock in the Snapshot method.
type Counter struct {
	mu    sync.Mutex
	// Protected by mu:
	count int
	total int64
}

func (c *Counter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
	c.total += int64(c.count)
}

// BUG(check-ownership): reads count and total without holding mu.
func (c *Counter) Snapshot() (int, int64) {
	return c.count, c.total // race: no lock
}

// --- BUG: check-wg ---
// Pool manages a set of workers; Add is called inside the goroutine body.
type Pool struct {
	wg sync.WaitGroup
}

func (p *Pool) Start(n int) {
	for i := 0; i < n; i++ {
		go func(id int) {
			p.wg.Add(1) // BUG(check-wg): Add called inside goroutine, not before go
			defer p.wg.Done()
			fmt.Printf("worker %d running\n", id)
		}(i)
	}
	p.wg.Wait()
}

// --- BUG: check-defer-unlock ---
// ProcessBatch processes items under a mutex, but defers Unlock inside the loop.
type BatchProcessor struct {
	mu    sync.Mutex
	items []string
}

func (b *BatchProcessor) ProcessBatch() {
	for _, item := range b.items {
		b.mu.Lock()
		defer b.mu.Unlock() // BUG(check-defer): defer inside for-range loop
		fmt.Println("processing", item)
	}
}

// --- BUG: check-locks ---
// muAccount and muLedger are package-level mutexes shared by the two transfer
// functions below, which acquire them in opposite orders — classic AB/BA deadlock.
var (
	muAccount sync.Mutex
	muLedger  sync.Mutex
)

var (
	balance int
	entries []string
)

// Order: muAccount → muLedger
func Transfer(amount int) {
	muAccount.Lock()
	defer muAccount.Unlock()
	muLedger.Lock()
	defer muLedger.Unlock()
	balance -= amount
	entries = append(entries, fmt.Sprintf("debit %d", amount))
}

// Order: muLedger → muAccount  — BUG(check-locks): inverted from Transfer
func RecordAndDebit(amount int) {
	muLedger.Lock()
	defer muLedger.Unlock()
	muAccount.Lock()
	defer muAccount.Unlock()
	balance -= amount
	entries = append(entries, fmt.Sprintf("record %d", amount))
}
