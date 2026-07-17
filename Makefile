.PHONY: build install fmt fmt-check vet lint test test-race coverage check release-snapshot demo

GO ?= go
BINARY ?= revui

build:
	$(GO) build -trimpath -o $(BINARY) ./cmd/revui

install:
	$(GO) install ./cmd/revui

fmt:
	gofmt -w cmd internal

fmt-check:
	@test -z "$$(gofmt -l cmd internal)" || (echo "Run 'make fmt' on:"; gofmt -l cmd internal; exit 1)

vet:
	$(GO) vet ./...

lint:
	golangci-lint run

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

coverage:
	./scripts/check-coverage.sh

check: fmt-check vet test

release-snapshot:
	goreleaser release --snapshot --clean

demo: build
	vhs docs/demo.tape
