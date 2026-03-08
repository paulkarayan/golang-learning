package testdata

// Pattern 4: Defer sends on a channel whose receiver may be dead
// BUG: The defer sends a completion signal, but if the parent already
// returned due to context cancellation, the channel has no reader.

func Worker(done chan struct{}) {
	defer func() {
		done <- struct{}{} // BUG: blocks if receiver is gone
	}()

	// Do some work...
	heavyWork()
}

func heavyWork() {
	// simulate work
}
