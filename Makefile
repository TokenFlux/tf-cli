BINARY := tkr
PKG    := github.com/tokenflux/tkr
VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w \
	-X $(PKG)/internal/buildinfo.Version=$(VERSION) \
	-X $(PKG)/internal/buildinfo.Commit=$(COMMIT)

.PHONY: build test fmt vet check clean install

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/tkr

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

# CI 与提交前的统一入口。
check: fmt vet test

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/tkr

clean:
	rm -rf bin
