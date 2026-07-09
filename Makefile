.PHONY: build test check install

build:
	go build -o revui ./cmd/revui

test:
	go test ./...

check:
	gofmt -w cmd internal
	go vet ./...
	go test ./...

install:
	go install ./cmd/revui
