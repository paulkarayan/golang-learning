.PHONY: test race lint vet fmt fmt-check semgrep gcatch stress ci ci-full setup-hooks

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

## Run stress tests (race + count=100, build tag)
stress:
	go test -tags=stress -race -count=100 -timeout=15m ./...

## Run full CI pipeline locally
ci: fmt-check vet lint semgrep race

## Run everything including stress (slow)
ci-full: ci stress

## Set up git hooks
setup-hooks:
	git config core.hooksPath .githooks
	@echo "Git hooks configured to use .githooks/"
