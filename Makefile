BINARY := tf
PKG    := github.com/tokenflux/tkr
VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

# 自建 TokenRouter 在这里定死默认网关，团队里的人照常 tf login 即可：
#   make build HOST=https://router.acme.com
HOST ?=
LDFLAGS := -s -w \
	-X $(PKG)/internal/buildinfo.Version=$(VERSION) \
	-X $(PKG)/internal/buildinfo.Commit=$(COMMIT)
ifneq ($(HOST),)
LDFLAGS += -X $(PKG)/internal/config.DefaultHost=$(HOST)
endif

.PHONY: build test fmt vet check cross clean install

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/tf

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

# CI 与提交前的统一入口。
check: fmt vet test

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/tf

clean:
	rm -rf bin

# 发布矩阵里的每个目标都要能编译。本机 go build 看不到这类问题。
cross:
	@for t in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/amd64; do 		GOOS=$${t%/*} GOARCH=$${t#*/} CGO_ENABLED=0 go build -o /dev/null ./cmd/tf 			&& echo "  ok $$t" || exit 1; 	done
