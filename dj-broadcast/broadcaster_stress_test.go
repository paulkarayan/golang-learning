//go:build stress

package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// Catches TOCTOU between Get and Read — if Stop runs between
// sm.Get() returning and b.Read() being called, Read should not block forever
func TestRace_GetReadVsStop(t *testing.T) {
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
				cursor := 0
				b.Read(context.Background(), &cursor)
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
			t.Fatal("Read blocked after Stop — TOCTOU bug detected")
		default:
		}
	}
}

// Probes double-close panic — calling Close() concurrently from multiple goroutines
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

// probes goroutine leaks from slow readers that never consume
func TestLeak_SlowReader(t *testing.T) {
	b := NewBroadcaster()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cursor := 0
			// read once then abandon — simulates slow/disconnected client
			b.Read(context.Background(), &cursor)
		}()
	}

	for i := 0; i < 1000; i++ {
		b.Send([]byte("data"))
	}

	b.Close()
	wg.Wait()
}

// All blocked readers must wake up when Close is called
// there should be no deadlocks observed
func TestStress_CloseWakesAllReaders(t *testing.T) {
	for i := 0; i < 100; i++ {
		b := NewBroadcaster()
		var wg sync.WaitGroup

		for j := 0; j < 50; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cursor := 0
				b.Read(context.Background(), &cursor)
			}()
		}

		time.Sleep(10 * time.Millisecond)
		b.Close()

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("not all readers woke up after Close")
		}
	}
}

// Concurrent readers and writers don't deadlock under load
func TestStress_ConcurrentReadWrite(t *testing.T) {
	b := NewBroadcaster()

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				b.Send([]byte("data"))
			}
		}()
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cursor := 0
			for {
				_, ok := b.Read(context.Background(), &cursor)
				if !ok {
					return
				}
			}
		}()
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		b.Close()
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: concurrent read/write didn't complete")
	}
}

// Rapid connect/disconnect under load doesn't leak goroutines
func TestStress_RapidConnectDisconnect(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	go func() {
		for i := 0; i < 10000; i++ {
			b.Send([]byte("data"))
			time.Sleep(time.Microsecond)
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(
				context.Background(),
				time.Duration(rand.Intn(10))*time.Millisecond,
			)
			defer cancel()

			go func() {
				<-ctx.Done()
				b.cond.Broadcast()
			}()

			cursor := 0
			for {
				_, ok := b.Read(ctx, &cursor)
				if !ok {
					return
				}
			}
		}()
	}
	wg.Wait()
}

// Catches panic when Close() races with in-flight Send/Read
func TestRace_ConcurrentOpsVsClose(t *testing.T) {
	for i := 0; i < 200; i++ {
		b := NewBroadcaster()
		var wg sync.WaitGroup

		// readers
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cursor := 0
				b.Read(context.Background(), &cursor)
			}()
		}

		// senders
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
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
