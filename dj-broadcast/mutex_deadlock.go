//go:build deadlock

package main

import "github.com/sasha-s/go-deadlock"

// Mutex swaps to go-deadlock for runtime deadlock detection.
type Mutex = deadlock.Mutex
