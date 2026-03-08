//go:build deadlock

package internal

import "github.com/sasha-s/go-deadlock"

// Mutex swaps to go-deadlock for runtime deadlock detection.
type Mutex = deadlock.Mutex
