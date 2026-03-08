package main

import "fmt"

// promptTemplates maps semgrep rule IDs (and grep-based pseudo-rule IDs) to
// LLM prompt templates. Each template receives contextual source code.
var promptTemplates = map[string]string{
	"closeable-type-inventory": `You are reviewing Go code for concurrency safety.

A type has a Close/Stop/Shutdown method. Below is the type definition and ALL public methods on that type.

%s

Questions:
1. What happens if each public method is called AFTER Close/Stop/Shutdown?
2. Is there a "done" flag or similar guard checked in each method?
3. Can a caller get a reference to this type and call methods while another goroutine calls Close?

Reply with ONLY findings that are actual bugs. For each bug:
- FILE: <file path>
- LINE: <line number>
- SEVERITY: BUG or WARNING
- SUMMARY: one-line description

If everything is safe, reply with exactly: NO_ISSUES_FOUND`,

	"channel-make-inventory": `You are reviewing Go code for channel safety.

A channel is created below. I'm showing the function where it's created and all code that sends to or receives from this channel.

%s

Questions:
1. Can the producer send on this channel after the consumer has stopped reading?
2. Can this channel deadlock (e.g., unbuffered channel where sender and receiver are in the same goroutine)?
3. Is the channel properly closed? Who closes it — producer or consumer?
4. For buffered channels: can the buffer fill up and block the sender unexpectedly?

Reply with ONLY findings that are actual bugs. For each bug:
- FILE: <file path>
- LINE: <line number>
- SEVERITY: BUG or WARNING
- SUMMARY: one-line description

If everything is safe, reply with exactly: NO_ISSUES_FOUND`,

	"mutex-lock-inventory": `You are reviewing Go code for lock ordering safety.

Multiple locks exist on the same type or in the same package. Below is the relevant code.

%s

Questions:
1. Are these locks ever acquired in different orders across different code paths?
2. Can a goroutine hold lock A and then try to acquire lock B, while another holds B and tries A?
3. Is any lock held across a blocking operation (channel send/receive, I/O, cond.Wait)?

Reply with ONLY findings that are actual bugs. For each bug:
- FILE: <file path>
- LINE: <line number>
- SEVERITY: BUG or WARNING
- SUMMARY: one-line description

If everything is safe, reply with exactly: NO_ISSUES_FOUND`,

	"defer-channel-send": `You are reviewing Go code for defer+channel safety.

A defer block sends on a channel. Below is the function containing this defer.

%s

Questions:
1. Can the receiver goroutine have exited by the time this defer runs?
2. If the channel is unbuffered, will this block forever if no one is reading?
3. If the channel is closed before the defer runs, will this panic?

Reply with ONLY findings that are actual bugs. For each bug:
- FILE: <file path>
- LINE: <line number>
- SEVERITY: BUG or WARNING
- SUMMARY: one-line description

If everything is safe, reply with exactly: NO_ISSUES_FOUND`,

	"fire-and-forget-goroutine": `You are reviewing Go code for goroutine lifecycle safety.

A goroutine is launched without clear coordination. Below is the function.

%s

Questions:
1. What signal tells this goroutine to stop?
2. Can this goroutine outlive its parent function or the object it references?
3. If the goroutine panics, is it recovered? Does the panic propagate?
4. Is there a WaitGroup, context, or done channel that gates shutdown?

Reply with ONLY findings that are actual bugs. For each bug:
- FILE: <file path>
- LINE: <line number>
- SEVERITY: BUG or WARNING
- SUMMARY: one-line description

If everything is safe, reply with exactly: NO_ISSUES_FOUND`,

	"toctou-map-lookup": `You are reviewing Go code for TOCTOU (time-of-check-time-of-use) races.

A value is retrieved from a map/store, then used later. Below is the relevant code.

%s

Questions:
1. Between retrieving the value and using it, can another goroutine modify or delete it?
2. Is the map protected by a lock? Is the lock held continuously from lookup through use?
3. Can the retrieved object become invalid (closed, stopped) between lookup and use?

Reply with ONLY findings that are actual bugs. For each bug:
- FILE: <file path>
- LINE: <line number>
- SEVERITY: BUG or WARNING
- SUMMARY: one-line description

If everything is safe, reply with exactly: NO_ISSUES_FOUND`,

	"context-propagation": `You are reviewing Go code for context cancellation propagation.

Functions that perform blocking operations should accept and respect context.Context. Below is code where blocking operations occur.

%s

Questions:
1. Does this function accept a context.Context parameter?
2. If it has a context, does it check ctx.Done() in select statements or loops?
3. Can this function block indefinitely if the context is cancelled?
4. Are child goroutines given derived contexts (context.WithCancel/Timeout)?

Reply with ONLY findings that are actual bugs. For each bug:
- FILE: <file path>
- LINE: <line number>
- SEVERITY: BUG or WARNING
- SUMMARY: one-line description

If everything is safe, reply with exactly: NO_ISSUES_FOUND`,
}

// buildPrompt formats a prompt template with the given context code.
func buildPrompt(ruleID, context string) string {
	tmpl, ok := promptTemplates[ruleID]
	if !ok {
		return fmt.Sprintf("Review this Go code for concurrency bugs:\n\n%s\n\nReport only real bugs with FILE, LINE, SEVERITY, SUMMARY. If safe, reply: NO_ISSUES_FOUND", context)
	}
	return fmt.Sprintf(tmpl, context)
}
