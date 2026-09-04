BINARY := tf
PKG    := github.com/tokenflux/tf-cli
VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
NPM_VERSION ?= 0.0.0-development.0
NPM_BINARY_DIR := dist/npm-binaries
NPM_OUTPUT_DIR := dist/npm

# 自建 TokenRouter 在这里定死默认网关，团队里的人照常 tf login 即可：
#   make build HOST=https://router.acme.com
HOST ?=
LDFLAGS := -s -w \
	-X $(PKG)/internal/buildinfo.Version=$(VERSION) \
	-X $(PKG)/internal/buildinfo.Commit=$(COMMIT)
ifneq ($(HOST),)
LDFLAGS += -X $(PKG)/internal/config.DefaultHost=$(HOST)
endif

.PHONY: build test fmt vet check cross clean install shell-test npm-test npm-pack npm-check

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/tf

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

# CI 与提交前的统一入口。
# check 成功时只留最后一行，失败时 make 自己会以非零退出。
#
# 加这一行是因为我习惯用 grep 筛 check 的输出，而 go vet 的
# 错误既不以 FAIL 也不以 --- 开头 —— 筛掉之后，一次编译不过的测试
# 一路过了本地、进了提交、红在 CI 上。结论要由命令自己说，不该靠
# 调用者挑着看。
check: fmt vet test shell-test
	@echo "check ok"

shell-test:
	sh -n install.sh uninstall.sh scripts/uninstall-test.sh
	sh scripts/uninstall-test.sh

# pty 测试用真的伪终端驱动真的二进制。
#
# 不进 check：CI 里没有 tty，而这些测试要先 make build。
# 它们抓的是单元测试抓不到的那一类 —— 按键、转义序列、进程边界。
.PHONY: pty
pty: build
	go test -tags pty ./e2e/ -v

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/tf

clean:
	rm -rf bin

# 发布矩阵里的每个目标都要能编译。本机 go build 看不到这类问题。
cross:
	@for t in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/amd64; do \
		GOOS=$${t%/*} GOARCH=$${t#*/} CGO_ENABLED=0 go build -o /dev/null ./cmd/tf \
			&& echo "  ok $$t" || exit 1; \
	done

npm-test:
	node --test npm/tests/*.test.cjs npm/tests/*.test.mjs

npm-pack:
	rm -rf $(NPM_BINARY_DIR) $(NPM_OUTPUT_DIR)
	@for t in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/amd64; do \
		goos=$${t%/*}; goarch=$${t#*/}; name=tf; \
		[ "$$goos" = windows ] && name=tf.exe; \
		out="$(NPM_BINARY_DIR)/npm-$$goos-$$goarch/$$name"; \
		mkdir -p "$$(dirname "$$out")"; \
		GOOS=$$goos GOARCH=$$goarch CGO_ENABLED=0 go build -trimpath \
			-ldflags "-s -w -X $(PKG)/internal/buildinfo.Version=$(NPM_VERSION) -X $(PKG)/internal/buildinfo.Commit=$(COMMIT)" \
			-o "$$out" ./cmd/tf || exit 1; \
	done
	node scripts/build-npm-packages.mjs $(NPM_VERSION) $(NPM_BINARY_DIR) $(NPM_OUTPUT_DIR)

npm-check: npm-test npm-pack
	node scripts/check-npm-packages.mjs $(NPM_OUTPUT_DIR)
	node scripts/publish-npm-packages.mjs $(NPM_OUTPUT_DIR) --tag development --dry-run
