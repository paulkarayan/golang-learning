.PHONY: test race lint vet fmt fmt-check semgrep gcatch stress deadlock ci ci-full setup-hooks llm-review review review-static _review-vet _review-lint _review-semgrep review-llm code-review security-review

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
	GO111MODULE=off GCatch -path=$$(pwd) -include=dj-broadcast -checker=BMOC -r

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

## --- Code Review Buddy pipeline ---

REVIEW_DIR := .review-output
BUDDY_SRC := ../codereview-buddy
BUDDY_BIN := $(BUDDY_SRC)/codereview-buddy

## Build the review buddy tool if needed
$(BUDDY_BIN): $(wildcard $(BUDDY_SRC)/*.go)
	cd $(BUDDY_SRC) && go build -o codereview-buddy .

## Run static analyzers in parallel
review-static:
	@mkdir -p $(REVIEW_DIR)
	@$(MAKE) -j3 _review-vet _review-lint _review-semgrep

_review-vet:
	@echo "running go vet..."
	@go vet ./... > $(REVIEW_DIR)/vet.txt 2>&1 || true
	@echo "go vet done"

_review-lint:
	@echo "running golangci-lint..."
	@golangci-lint run ./... > $(REVIEW_DIR)/lint.txt 2>&1 || true
	@echo "golangci-lint done"

_review-semgrep:
	@echo "running semgrep..."
	@semgrep --config .semgrep.yml --json . > $(REVIEW_DIR)/semgrep.json 2>/dev/null || true
	@echo "semgrep done"

## Triage semgrep findings + run LLM evaluation
review-llm: review-static $(BUDDY_BIN)
	@echo "triaging findings..."
	$(BUDDY_BIN) triage -semgrep $(REVIEW_DIR)/semgrep.json -dir . -o $(REVIEW_DIR)/prompts.json
	@echo "evaluating with LLM..."
	$(BUDDY_BIN) evaluate -prompts $(REVIEW_DIR)/prompts.json -o $(REVIEW_DIR)/findings.json

## Full review pipeline
review: review-llm
	$(BUDDY_BIN) report -findings $(REVIEW_DIR)/findings.json

## Review with markdown output (for PR comments)
review-md: review-llm
	$(BUDDY_BIN) report -findings $(REVIEW_DIR)/findings.json -format markdown

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
