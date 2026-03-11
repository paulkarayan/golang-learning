.PHONY: lint test fmt all-check code-review security-review # these are commands not files

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	golangci-lint run

test:
	go test ./...

check: fmt vet lint test

## Claude Code review
code-review:
	claude -p "/code-review"

## Claude security review
security-review:
	claude -p "/security-review"

PROJECTS := ./little-book-of-go ./snippetbox

all-check:
	@for dir in $(PROJECTS); do \
		echo "=== $$dir ===" && \
		(cd $$dir && go fmt ./... && go vet ./... && go test ./... && golangci-lint run) || exit 1; \
	done

## --- LLM concurrency reviews — two-pass (discover then analyze per subject) ---

GO_SRC_FILES := $(shell find . -name '*.go' -not -name '*_test.go' | tr '\n' ' ')

## Deadlock: find every lock site, then trace each for missing release / circular wait
llm-Deadlock:
	@echo "==> Pass 1: identifying lock acquisition sites..."; \
	claude -p "Read these Go files: $(GO_SRC_FILES). List every location where a mutex or condition variable is acquired or waited on. Output ONLY a newline-separated list, one per line, format: 'filepath:linenum: description'. No headers, no markdown." > /tmp/llm-deadlock-cases.txt; \
	echo "==> Found $$(grep -c . /tmp/llm-deadlock-cases.txt) sites. Analyzing each..."; \
	while IFS= read -r case; do \
		[ -z "$$case" ] && continue; \
		echo "\n--- $$case ---"; \
		claude -p "In a Go codebase, a mutex or condition variable is acquired here: '$$case'. Read the relevant file. Trace every code path from acquisition to release, step by step. Then assess: is there any path where the lock is never released, or where two goroutines could wait on each other indefinitely? Verdict: REAL BUG, FALSE POSITIVE, or NEEDS REVIEW."; \
	done < /tmp/llm-deadlock-cases.txt

## GoroutineLifetime: find every spawned goroutine, then trace each for outliving its owner
llm-GoroutineLifetime:
	@echo "==> Pass 1: identifying spawned goroutines..."; \
	claude -p "Read these Go files: $(GO_SRC_FILES). List every goroutine spawned in the codebase. Output ONLY a newline-separated list, one per line, format: 'filepath:linenum: description'. No headers, no markdown." > /tmp/llm-goroutine-cases.txt; \
	echo "==> Found $$(grep -c . /tmp/llm-goroutine-cases.txt) goroutines. Analyzing each..."; \
	while IFS= read -r case; do \
		[ -z "$$case" ] && continue; \
		echo "\n--- $$case ---"; \
		claude -p "In a Go codebase, a goroutine is spawned here: '$$case'. Read the relevant file. Trace its full lifecycle step by step: when it is spawned, what it blocks on, and what causes it to exit. Then assess: can it outlive its owner, or run after the owning object is no longer valid? Verdict: REAL BUG, FALSE POSITIVE, or NEEDS REVIEW."; \
	done < /tmp/llm-goroutine-cases.txt

## PublicMethodsAfterDone: find every public method, then check each after background goroutine finishes
llm-PublicMethodsAfterDone:
	@echo "==> Pass 1: identifying public methods..."; \
	claude -p "Read these Go files: $(GO_SRC_FILES). List every exported method on JobManager. Output ONLY a newline-separated list, one per line, format: 'filepath:linenum: description'. No headers, no markdown." > /tmp/llm-publicmethods-cases.txt; \
	echo "==> Found $$(grep -c . /tmp/llm-publicmethods-cases.txt) methods. Analyzing each..."; \
	while IFS= read -r case; do \
		[ -z "$$case" ] && continue; \
		echo "\n--- $$case ---"; \
		claude -p "In a Go codebase, there is a public method here: '$$case'. Read the relevant file. Trace what happens when this method is called after the background goroutine for a job has already finished, step by step. Then assess: does it return correct results, or is there any incorrect behavior? Verdict: REAL BUG, FALSE POSITIVE, or NEEDS REVIEW."; \
	done < /tmp/llm-publicmethods-cases.txt

## ConcurrentMutations: find every mutating method, then trace each for concurrent-caller hazards
llm-ConcurrentMutations:
	@echo "==> Pass 1: identifying mutating methods..."; \
	claude -p "Read these Go files: $(GO_SRC_FILES). List every public method that mutates job state. Output ONLY a newline-separated list, one per line, format: 'filepath:linenum: description'. No headers, no markdown." > /tmp/llm-mutations-cases.txt; \
	echo "==> Found $$(grep -c . /tmp/llm-mutations-cases.txt) methods. Analyzing each..."; \
	while IFS= read -r case; do \
		[ -z "$$case" ] && continue; \
		echo "\n--- $$case ---"; \
		claude -p "In a Go codebase, there is a mutating method here: '$$case'. Read the relevant file. Trace what happens when two goroutines call this method on the same job ID concurrently, walking through the steps of both callers interleaved. Then assess: is there any interleaving that produces a panic, data corruption, or incorrect error? Verdict: REAL BUG, FALSE POSITIVE, or NEEDS REVIEW."; \
	done < /tmp/llm-mutations-cases.txt

## TOCTOU: find every lookup-then-act site, then trace each for gap hazards
llm-TOCTOU:
	@echo "==> Pass 1: identifying lookup-then-act sites..."; \
	claude -p "Read these Go files: $(GO_SRC_FILES). List every location where a lookup is followed by an action that depends on the result of that lookup. Output ONLY a newline-separated list, one per line, format: 'filepath:linenum: description'. No headers, no markdown." > /tmp/llm-toctou-cases.txt; \
	echo "==> Found $$(grep -c . /tmp/llm-toctou-cases.txt) sites. Analyzing each..."; \
	while IFS= read -r case; do \
		[ -z "$$case" ] && continue; \
		echo "\n--- $$case ---"; \
		claude -p "In a Go codebase, there is a lookup followed by an action here: '$$case'. Read the relevant file. Trace what can change between the lookup and the action, step by step. Then assess: is there any scenario where state changes in that gap in a way that causes incorrect behavior? Verdict: REAL BUG, FALSE POSITIVE, or NEEDS REVIEW."; \
	done < /tmp/llm-toctou-cases.txt

## Run all five LLM concurrency reviews sequentially
llm-comprehensive: llm-Deadlock llm-GoroutineLifetime llm-PublicMethodsAfterDone llm-ConcurrentMutations llm-TOCTOU