Review all Go files in $ARGUMENTS for concurrency bugs. If no arguments given, review all .go files (excluding test files and generated protobuf files).

You are a Go concurrency expert. For each file, independently analyze:

1. **Data races and TOCTOU patterns** — check-then-act across unlock boundaries, unprotected shared state
2. **Missing or incorrect networking error handling** — unchecked errors on Close, Read, Write, Dial
3. **Goroutine leaks** — fire-and-forget goroutines, blocked channel sends/receives, missing cleanup
4. **Thread safety** — HTTP handler functions, global variables, struct fields accessed across goroutines
5. **Channel misuse** — unbuffered vs buffered capacity, double-close, sends on closed channels
6. **Mutex issues** — missing unlocks before return, lock ordering, unnecessary lock scope
7. **Patterns invisible to tooling** — issues that won't be caught by golangci-lint, go vet, or go test -race

Checklist to verify:
- Are HTTP handler functions thread-safe?
- Are global functions and variables protected by mutexes or otherwise thread-safe?
- Are reads of fields and variables protected?
- Are loop variables passed into goroutine functions as arguments?
- Do methods on thread-safe types return pointers to protected structures? (they shouldn't)
- Are sync.Map Load/Store/Delete sequences race-free?
- Are channels intentionally created with zero vs buffered capacity?
- Are time.Ticker instances stopped with defer tick.Stop()?

Report ONLY real issues with file path, line number, and a brief explanation. No false positives. If a file has no issues, say so in one line.
