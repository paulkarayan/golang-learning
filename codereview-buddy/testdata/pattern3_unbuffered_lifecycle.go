package testdata

import (
	"fmt"
	"time"
)

// Pattern 3: Unbuffered channel — sender outlives receiver
// BUG: The goroutine sends on an unbuffered channel, but the receiver
// may have returned (e.g., due to timeout), leaving the sender blocked forever.

func FetchWithTimeout() (string, error) {
	result := make(chan string) // unbuffered

	go func() {
		// Simulate slow work
		time.Sleep(5 * time.Second)
		result <- "done" // BUG: blocks forever if caller timed out
	}()

	select {
	case r := <-result:
		return r, nil
	case <-time.After(1 * time.Second):
		return "", fmt.Errorf("timeout")
		// goroutine leaks — nobody reads from result
	}
}
