package testdata

import "fmt"

// Pattern 2: Buffered channel saturation / self-deadlock
// BUG: Producer and consumer are in the same goroutine with a small buffer.

func ProcessItems(items []string) {
	results := make(chan string, 2) // small buffer

	// BUG: If len(items) > 2, this blocks because the buffer fills up
	// and nobody is reading from results yet.
	for _, item := range items {
		results <- fmt.Sprintf("processed: %s", item)
	}
	close(results)

	for r := range results {
		fmt.Println(r)
	}
}
