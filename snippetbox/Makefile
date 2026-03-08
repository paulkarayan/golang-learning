.PHONY: test race lint vet fmt fmt-check semgrep gcatch stress deadlock ci ci-full setup-hooks llm-review code-review security-review

## Run unit tests
test:
	go test ./...

## Run tests with race detector (count=10)
race:
	go test -race -count=10 ./...

## Run golangci-lint
lint:
	golangci-lint run ./...

## Run go vet
vet:
	go vet ./...

## Format code in place
fmt:
	gofmt -w .

## Check formatting (fails if unformatted)
fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "Files not formatted:"; gofmt -l .; exit 1)

## Run semgrep with custom + community rules
semgrep:
	semgrep --config .semgrep.yml --config p/golang --config https://semgrep.dev/p/trailofbits .

## Run GCatch static concurrency analysis (requires Z3 + GCatch in GOPATH)
gcatch:
	GO111MODULE=off GCatch -path=$$(pwd) -include=snippetbox.paulkarayan.com -checker=BMOC -r

## Run tests with deadlock detection
deadlock:
	go test -tags=deadlock -race -count=5 -timeout=5m ./...

## Run stress tests (race + count=100, build tag)
stress:
	go test -tags=stress -race -count=100 -timeout=15m ./...

## Run full CI pipeline locally
ci: fmt-check vet lint semgrep race

## Run everything including stress (slow)
ci-full: ci deadlock stress

## LLM concurrency review via Claude Code
llm-review:
	@FILES=$$(find . -name '*.go' -not -name '*_test.go' -not -path './cmd/proto/*' | tr '\n' ' '); \
	claude -p "Review these Go files for concurrency bugs: $$FILES. Read each file and check for: data races, TOCTOU patterns, goroutine leaks, missing error handling on Close/Read/Write/Dial, unprotected shared state, channel misuse, mutex issues. Report ONLY real issues with file path, line number, and brief explanation. No false positives."

## Claude Code review
code-review:
	claude -p "/code-review"

## Claude security review
security-review:
	claude -p "/security-review"

## Set up git hooks
setup-hooks:
	git config core.hooksPath .githooks
	@echo "Git hooks configured to use .githooks/"
