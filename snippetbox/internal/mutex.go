//go:build !deadlock

package internal

import "sync"

// Mutex is sync.Mutex by default; use -tags=deadlock for deadlock detection.
type Mutex = sync.Mutex
