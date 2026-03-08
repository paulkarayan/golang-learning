package testdata

import "sync"

// Pattern 7: Inconsistent lock acquisition order
// BUG: transfer() acquires lockA then lockB, but audit() acquires lockB then lockA.
// This is a classic deadlock.

type Account struct {
	mu      sync.Mutex
	balance int
}

type Bank struct {
	accountA Account
	accountB Account
}

// Acquires A then B
func (b *Bank) Transfer(amount int) {
	b.accountA.mu.Lock()
	defer b.accountA.mu.Unlock()

	b.accountB.mu.Lock()
	defer b.accountB.mu.Unlock()

	b.accountA.balance -= amount
	b.accountB.balance += amount
}

// BUG: Acquires B then A — opposite order from Transfer()
func (b *Bank) Audit() int {
	b.accountB.mu.Lock()
	defer b.accountB.mu.Unlock()

	b.accountA.mu.Lock()
	defer b.accountA.mu.Unlock()

	return b.accountA.balance + b.accountB.balance
}
