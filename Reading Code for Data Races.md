Reading Code for Data Races
The "who owns this variable?" question
For every shared variable, ask: is there a single clear owner? A data race almost always means the answer is "it's ambiguous." If you can't immediately point to one goroutine or one mutex that owns a value, that's a red flag.
The "is this closed over?" scan
Goroutine closures that capture loop variables or shared pointers are the most common source of races:
gofor _, v := range items {
    go func() {
        process(v) // race: v is shared across all goroutines
    }()
}
Any time you see go func() without arguments, scan upward for what variables it's closing over.
The "is this map concurrent?" check
Go maps are not safe for concurrent access — but there's no type-level warning. Any map that's touched by more than one goroutine without a lock is a race. Search for maps that are declared outside a goroutine and written to inside one.
Read the struct, not just the function
A mutex that doesn't live next to the data it protects is suspicious. The canonical pattern is:
gotype Thing struct {
    mu    sync.Mutex
    value int  // mu protects value
}
If you see a mutex in one place and the data it supposedly guards scattered elsewhere, the protection is probably incomplete.

Reading Code for Deadlocks
The "lock order" scan
Deadlocks almost always come from acquiring two locks in inconsistent order across goroutines. When you see nested locks, draw out the order:
go// goroutine A: locks mu1 then mu2
// goroutine B: locks mu2 then mu1  ← deadlock waiting to happen
Any time you see a lock acquired while another is already held, ask: is there another code path that acquires these in reverse order?
The "who closes this channel?" rule
A channel that nobody closes, but something is range-ing over, will block forever. The sender should own closing. If you see for v := range ch and have to hunt to find where close(ch) is (or can't find it), that's a leak.
The "unbuffered channel send with no receiver" check
goch := make(chan int)
ch <- value  // blocks forever if nobody is receiving
Unbuffered sends and receives are synchronization points. Ask: is the matching receive guaranteed to happen, or can it be skipped (e.g. due to an early return or a select with a default)?
The "select with no default" audit
A select that blocks on multiple channels will wait until one is ready. If all channels are drained or nil, it blocks forever. Ask whether each case can realistically fire.

Visualisation Strategies
Draw the goroutine graph
Sketch boxes for each goroutine and arrows for each channel between them. A goroutine with arrows going in but none coming out (or vice versa) is often a leak or deadlock waiting to happen. This is especially useful for pipelines.
Colour the data
Go through the code and mentally colour each piece of shared state: green if it's always accessed under a lock or owned by one goroutine, red if it's accessed from multiple goroutines without clear protection. The goal is a fully green codebase.
Trace the lifecycle of goroutines
For every go func(...), ask: where does this goroutine end? A goroutine that has no clear exit path is a leak. Common traps are goroutines that block on a channel send where the receiver has already returned, or that loop waiting for a context that never gets cancelled.
The happens-before chain
For subtle races, ask: is there a guaranteed ordering between the write and the read? In Go, the guarantees are: channel sends happen before receives, sync.WaitGroup.Done happens before Wait returns, and mutex unlock happens before the next lock. If you can't draw a chain of these guarantees from the write to the read, assume it's a race.

General Heuristics

More goroutines than channels: probably over-using shared memory, higher race risk
Goroutine spawned in a loop: always scrutinise carefully
sync.WaitGroup Add called inside the goroutine: almost always wrong — Add must be called before go
Returning a pointer to a local, then sharing it: the local is now shared; any mutation is unsafe without a lock
defer mu.Unlock() inside a loop: the unlock won't fire until the function returns, not each iteration — a common bug that holds a lock far longer than intended

The single most useful habit is just asking "who owns this?" for every piece of mutable state. If the answer is clear, you're usually fine. If it's vague, dig deeper.

--- why it's hard

Why It's Hard Statically
Goroutines are first-class runtime values, not static structure
Channels are passed around like any other variable. A goroutine might receive a channel as a parameter, store it in a struct, pass it to another function, and eventually send on it. Statically tracing "what connects to what" requires solving the same problem as full pointer/alias analysis, which is expensive and approximate even for simple programs. The graph you want to draw doesn't really exist in the source — it emerges at runtime.
Goroutine spawn is dynamic
gofor i := 0; i < n; i++ {   // n unknown at compile time
    go worker(ch)
}
How many boxes do you draw? The number of goroutines is often determined by config, input, or runtime state. Any static graph is necessarily an approximation or a type-level abstraction ("zero or more instances of this goroutine type").
The module boundary idea has a flaw
Decomposing at module level assumes goroutines and channels respect module boundaries — but they almost never do. A channel created in package A gets passed to package B and closed in package C. The interesting concurrency structure cuts across modules, not along them. You'd end up with a graph where most edges are "escaped to another package" which tells you very little.
Select makes control flow non-linear
goselect {
case v := <-ch1:
case v := <-ch2:
case outCh <- val:
}
This is a non-deterministic choice between communication operations. Statically you have to represent all possible paths, which causes the graph to explode in size for any real program.
Closures capture variables invisibly
A goroutine closure might close over a channel or mutex without it appearing in any function signature. Static tools have to do escape analysis to even know what a goroutine has access to, let alone reason about it correctly.

--- Restrictions That Are Actually Enforceable Today


That said, there are enforceable conventions that get you most of the way there statically:
One owner per channel — the goroutine that creates a channel is the only sender; receivers are explicit. If you enforce make(chan T) only appears in the goroutine that owns sending, a linter could check this. go vet doesn't, but you could write a custom analysis pass.
Channels as struct fields only — never pass channels as bare function arguments, only embed them in structs with documented direction. Combined with chan<- / <-chan directional types (which Go already has), this makes ownership visible in signatures.
No channel in interface — if channels never appear in interface methods, they can't escape to unknown implementations, which makes tracking them tractable.
chan<- / <-chan at all boundaries — Go already has directional channel types but they're underused. If you enforce that every function parameter and return value uses directional channels, the ownership graph becomes explicit in the type signatures and is statically checkable.
Close ownership rule — only the sender closes, never the receiver. Expressible as a lint rule.

--- ideas of checks

Who owns this variable? — give LLM a struct/package, ask it to annotate ownership
Is this closed over? — mechanical scan of goroutine closures
Is this map concurrent? — find maps declared outside goroutines, written inside
Mutex colocation — find mutexes not living next to the data they protect
Lock order — trace nested lock acquisitions across goroutines
Who closes this channel? — trace channel lifecycle
WaitGroup.Add placement — mechanical check
defer mu.Unlock() in loop — mechanical check
Happens-before chain — harder, requires reasoning, but LLM can narrate it

--- human in the loop strategies

Visualisation Strategies
Draw the goroutine graph
Sketch boxes for each goroutine and arrows for each channel between them. A goroutine with arrows going in but none coming out (or vice versa) is often a leak or deadlock waiting to happen. This is especially useful for pipelines.
Colour the data

Go through the code and mentally colour each piece of shared state: green if it's always accessed under a lock or owned by one goroutine, red if it's accessed from multiple goroutines without clear protection. The goal is a fully green codebase.

Trace the lifecycle of goroutines
For every go func(...), ask: where does this goroutine end? A goroutine that has no clear exit path is a leak. Common traps are goroutines that block on a channel send where the receiver has already returned, or that loop waiting for a context that never gets cancelled.

The happens-before chain
For subtle races, ask: is there a guaranteed ordering between the write and the read? In Go, the guarantees are: channel sends happen before receives, sync.WaitGroup.Done happens before Wait returns, and mutex unlock happens before the next lock. If you can't draw a chain of these guarantees from the write to the read, assume it's a race.
