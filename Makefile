.PHONY: build install fmt fmt-check vet lint test test-race coverage check release-snapshot demo

GO ?= go
BINARY ?= revui
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_LINT_VERSION ?= 2.12.2

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
	@command -v $(GOLANGCI_LINT) >/dev/null || (echo "Install golangci-lint v$(GOLANGCI_LINT_VERSION): go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION)"; exit 1)
	@$(GOLANGCI_LINT) version | grep -q "version $(GOLANGCI_LINT_VERSION)" || (echo "golangci-lint $(GOLANGCI_LINT_VERSION) is required"; $(GOLANGCI_LINT) version; exit 1)
	$(GOLANGCI_LINT) run

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

coverage:
	./scripts/check-coverage.sh

check: fmt-check vet lint test

release-snapshot:
	goreleaser release --snapshot --clean

demo: build
	vhs docs/demo.tape
