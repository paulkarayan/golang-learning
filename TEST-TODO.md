# Test & CI/CD TODO — dj-broadcast + snippetbox

## Quick Reference: `go test -race -count=N`

`-count=N` controls how many times each test function runs. The race detector is **non-deterministic** — it instruments memory accesses and detects concurrent unsynchronized reads/writes, but only catches races that actually *happen* during execution.

- `-count=1`: each test runs once. Fast, but low probability of triggering timing-dependent races
- `-count=100`: each test runs 100 times. Massively increases the chance of hitting race windows because goroutine scheduling varies between runs. Think of it as a poor man's stress test — same test, 100 different scheduling rolls of the dice
- Tradeoff: `-count=100` takes ~100x longer. In CI, `-count=10` is a reasonable default for fast feedback; `-count=100` for nightly/stress runs

**Important limitation**: `-race` only catches **data races** (concurrent unsynchronized memory access). It does NOT catch:
- Logic races / TOCTOU bugs (check-then-act across unlock boundaries)
- Deadlocks
- Goroutine leaks
- Channel misuse (send on closed channel panics)

---

## Architecture: Multi-Layer CI Pipeline

```
┌─────────────────────────────────────────────────────────────────┐
│                         CI Pipeline                             │
│                                                                 │
│  Stage 1: Static Analysis (fast, cheap)                         │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐ │
│  │  golangci-lint   │  │     semgrep     │  │   go vet        │ │
│  │  (standard)      │  │  (custom rules) │  │                 │ │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘ │
│  Catches: copylocks, errcheck, some obvious issues              │
│  Misses: logic races, TOCTOU, coordination bugs                 │
│                                                                 │
│  Stage 2: Race Detector                                         │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  go test -race -count=10 ./...                            │   │
│  │  Catches: data races                                      │   │
│  │  Misses: logic races, deadlocks, TOCTOU                   │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Stage 3: Concurrency Stress Tests (THE KEY STAGE)              │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  go test -tags=stress -race -count=100 -timeout=15m ./...│   │
│  │  - Hammer concurrent operations with randomized timing    │   │
│  │  - Assert deadlines (catches blocking bugs / TOCTOU)      │   │
│  │  - Check goroutine counts (catches leaks)                 │   │
│  │  - Verify invariants after chaos                          │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Stage 4: Resource Leak Detection                               │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  goleak.VerifyNone(t) in TestMain (already in dj-broadcast)│  │
│  │  Memory profiling assertions                              │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Stage 5: LLM-based Concurrency Review (on changed files)      │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Two separate LLM passes:                                 │   │
│  │  1. Checklist pass (go-concurrency checklist as prompt)   │   │
│  │  2. Per-file race analysis (no shared context)            │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## What Catches What (Learnings Matrix)

| Issue | golangci-lint | race detector | semgrep | stress test | goleak |
|-------|:---:|:---:|:---:|:---:|:---:|
| TOCTOU Get→Subscribe | - | - | - | YES | - |
| Double close panic | - | - | YES | YES | - |
| Slow client goroutine leak | - | - | - | YES | YES |
| Unbounded history growth | - | - | - | YES (memory) | - |
| Orphaned goroutine (fire-and-forget) | - | - | YES | YES | - |
| No graceful shutdown | - | - | YES | - | - |
| Close vs Get panic | - | - | - | YES | - |


---

## TODO Items

### 1. Stress Tests (dj-broadcast) — bugs confirmed
- [ ] Fix TOCTOU bug: Subscribe blocks forever after Stop (confirmed by TestRace_GetSubscribeVsStop)
- [ ] Fix double-close bug: Close() hangs when called concurrently (confirmed by TestRace_DoubleClose)
- [ ] After fixes, run full stress suite to verify remaining tests pass

### 3. Fix confirmed dj-broadcast bugs, then verify stress suite passes end-to-end

