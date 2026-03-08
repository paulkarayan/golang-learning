# coming up with a better golang code review buddy


## ideas to think about
- "what happens when a buffered channel fills up.
- "What happens to every public method when the monitor goroutine is dead?"
- "What breaks if a channel is full?"
- "What happens if there's a deadlock?"
- "For every goroutine, can it outlive its owner?"

---

1. Public methods after shutdown
  Find every public method on types that have a Close()/Stop()/Shutdown().
  For each one, trace: what happens if the internal goroutine is dead? Does
  it block on a channel send? Does it panic on a closed channel? Does it
  read stale state?

  2. Buffered channel saturation
  Find every make(chan T, N). Trace: what happens when the buffer fills?
  Who's the producer, who's the consumer? Can the producer ever outpace the
  consumer? Is the producer in the same goroutine that needs to unblock the
  consumer? (That's Bug 1 — self-deadlock.)

  3. Unbuffered channel lifecycle
  Find every make(chan T) (no buffer). Trace: can the sender outlive the
  receiver? Can the receiver outlive the sender? After shutdown, is anyone
  still trying to send?

  4. Defer + channel sends
  Find every defer that sends on a channel or calls a method that sends on a
   channel. Trace: can the receiver be dead by the time the defer runs?
  (That's Bug 2.)

  5. TOCTOU on lookups
  Find every pattern like get → use. Between the get and the use, can
  another goroutine invalidate the result? Common in map lookups where you
  get a reference, then the map entry gets deleted/closed.

  6. Goroutine lifecycle
  For every go func() or go method(): what makes it stop? Is there always a
  path to termination? Can it block forever on a channel/lock? Who's
  responsible for cleanup?

  7. Lock ordering
  If you have multiple mutexes, is there a consistent acquisition order? Can
   goroutine A hold lock 1 and wait for lock 2 while goroutine B holds lock
  2 and waits for lock 1?

  8. Error paths on I/O
  Every conn.Close(), resp.Body.Close(), file.Close() — is the error
  checked? More importantly: on the error path of a function, are resources
  still cleaned up? Does a failed Read still close the connection?

  9. Context cancellation propagation
  Find every context.Context parameter. Does cancellation actually reach the
   blocking operation? Or does the code block on a channel send that ignores
   the context?

  10. sync primitives misuse
  - sync.WaitGroup.Add() called inside the goroutine instead of before it
  - sync.Once — is the function idempotent? What if it panics?
  - sync.Mutex — returned while still holding the lock? Recursive lock
  attempts?

  ---
  You could literally make a semgrep rule or a Go analysis pass for patterns
   1-5. Patterns 4 and 5 are especially amenable to static analysis — defer
  + channel send is greppable, and TOCTOU on map lookups follows a
  predictable AST shape.

---


  Pattern: 1. Public methods after shutdown
  go vet: -
  golangci-lint: -
  semgrep (your rules): -
  go test -race: -
  goleak: -
  GCatch: partial
  ────────────────────────────────────────
  Pattern: 2. Buffered channel saturation
  go vet: -
  golangci-lint: -
  semgrep (your rules): partial (unbounded-append)
  go test -race: -
  goleak: -
  GCatch: yes
  ────────────────────────────────────────
  Pattern: 3. Unbuffered channel lifecycle
  go vet: -
  golangci-lint: -
  semgrep (your rules): -
  go test -race: -
  goleak: -
  GCatch: yes
  ────────────────────────────────────────
  Pattern: 4. Defer + channel sends
  go vet: -
  golangci-lint: -
  semgrep (your rules): -
  go test -race: -
  goleak: catches the leak
  GCatch: yes
  ────────────────────────────────────────
  Pattern: 5. TOCTOU on lookups
  go vet: -
  golangci-lint: -
  semgrep (your rules): -
  go test -race: sometimes
  goleak: -
  GCatch: -
  ────────────────────────────────────────
  Pattern: 6. Goroutine lifecycle
  go vet: -
  golangci-lint: -
  semgrep (your rules): partial (fire-and-forget)
  go test -race: -
  goleak: yes
  GCatch: -
  ────────────────────────────────────────
  Pattern: 7. Lock ordering
  go vet: -
  golangci-lint: -
  semgrep (your rules): -
  go test -race: sometimes
  goleak: -
  GCatch: yes
  ────────────────────────────────────────
  Pattern: 8. Error paths on I/O
  go vet: -
  golangci-lint: errcheck, bodyclose
  semgrep (your rules): -
  go test -race: -
  goleak: -
  GCatch: -
  ────────────────────────────────────────
  Pattern: 9. Context cancellation
  go vet: -
  golangci-lint: noctx
  semgrep (your rules): -
  go test -race: -
  goleak: -
  GCatch: -
  ────────────────────────────────────────
  Pattern: 10. sync primitives misuse
  go vet: copylocks
  golangci-lint: govet/copylocks
  semgrep (your rules): -
  go test -race: sometimes
  goleak: -
  GCatch: -

  What's already well-covered:
  - 8 (errcheck, bodyclose) and 9 (noctx) — golangci-lint handles these
  - 10 — go vet catches copying locks, race detector catches some WaitGroup
  misuse
  - 6 — goleak catches leaked goroutines, your semgrep rule catches
  fire-and-forget

  What's partially covered:
  - 5 (TOCTOU) — race detector catches it only if the race actually fires
  during the test run. Misses timing-dependent ones.
  - 7 (lock ordering) — race detector catches data races from bad locking,
  but not deadlocks from lock ordering

  What's NOT covered by any existing tool:
  - 1 (public methods after shutdown) — nothing checks this. This is the gap
   the LLM filled. GCatch theoretically does but it's broken with modern Go.
  - 2 (buffer saturation self-deadlock) — GCatch was designed for this but
  is dead. Nothing else catches it.
  - 3 (unbuffered channel lifecycle) — same, GCatch territory.
  - 4 (defer + channel send to dead receiver) — goleak catches the symptom
  (leaked goroutine) but not the cause. Nothing identifies the defer as the
  problem.

  Bottom line: Patterns 1, 2, 3, and 4 are the gap. Those are the ones only
  the LLM review or GCatch (if it worked) would catch. Everything else has
  tooling coverage already. That's exactly where make llm-review earns its
  keep.

---


❯ partial coverage is not real coverage.

⏺ You're right. Here's what's actually covered vs not:

  Covered:
  - 8 (I/O error paths) — errcheck, bodyclose
  - 9 (context propagation) — noctx
  - 10 (copying locks) — go vet copylocks

  Not covered:
  - 1 (public methods after shutdown)
  - 2 (buffered channel saturation)
  - 3 (unbuffered channel lifecycle)
  - 4 (defer + channel sends to dead receiver)
  - 5 (TOCTOU on lookups)
  - 6 (goroutine lifecycle)
  - 7 (lock ordering / deadlocks)

  The race detector and goleak only catch things if the exact timing happens
   during the test run. That's detection, not coverage. Your semgrep
  fire-and-forget rule flags a pattern but doesn't trace whether the
  goroutine actually terminates.

  7 out of 10 patterns have no real tooling. That's the case for make
  llm-review.
