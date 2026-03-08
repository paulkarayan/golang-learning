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