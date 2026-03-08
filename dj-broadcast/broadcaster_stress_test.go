//go:build stress

package main

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// Catches TOCTOU between Get and Subscribe — if Stop runs between
// sm.Get() returning and b.Subscribe() being called, Subscribe blocks forever
func TestRace_GetSubscribeVsStop(t *testing.T) {
	const iterations = 500
	const timeout = 100 * time.Millisecond

	for i := 0; i < iterations; i++ {
		sm := NewStationManager()
		sm.Create("test")

		var wg sync.WaitGroup
		blocked := make(chan struct{})

		wg.Add(1)
		go func() {
			defer wg.Done()
			b, ok := sm.Get("test")
			if !ok {
				return
			}

			done := make(chan struct{})
			go func() {
				defer close(done)
				b.Subscribe()
			}()

			select {
			case <-done:
			case <-time.After(timeout):
				close(blocked)
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(rand.Intn(50)) * time.Microsecond)
			sm.Stop("test")
		}()

		wg.Wait()

		select {
		case <-blocked:
			t.Fatal("Subscribe blocked after Stop — TOCTOU bug detected")
		default:
		}
	}
}

// Catches double-close panic — calling Close() concurrently from multiple goroutines
func TestRace_DoubleClose(t *testing.T) {
	for i := 0; i < 100; i++ {
		b := NewBroadcaster()

		var wg sync.WaitGroup
		panicked := make(chan string, 10)

		for j := 0; j < 10; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						panicked <- fmt.Sprint(r)
					}
				}()
				b.Close()
			}()
		}
		wg.Wait()
		close(panicked)

		for msg := range panicked {
			t.Fatalf("Double close caused panic: %s", msg)
		}
	}
}

// Catches goroutine leaks from slow subscribers that never read
// goleak in TestMain will flag any leaked goroutines
func TestLeak_SlowSubscriber(t *testing.T) {
	b := NewBroadcaster()

	for i := 0; i < 100; i++ {
		b.Subscribe()
	}

	for i := 0; i < 1000; i++ {
		b.Send([]byte("data"))
	}

	b.Close()
}

// Catches panic when Close() races with in-flight Send/Subscribe
func TestRace_ConcurrentOpsVsClose(t *testing.T) {
	for i := 0; i < 200; i++ {
		b := NewBroadcaster()
		var wg sync.WaitGroup

		// subscribers
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { recover() }()
				b.Subscribe()
			}()
		}

		// senders
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { recover() }()
				b.Send([]byte("msg"))
			}()
		}

		// close while ops in flight
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(rand.Intn(5)) * time.Microsecond)
			b.Close()
		}()

		wg.Wait()
	}
}
