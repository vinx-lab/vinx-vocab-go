# Vinx Vocab 单文件版构建
# make test       Go 单测（设了 VINX_TEST_PG_URL 时连 PostgreSQL 导入测试一起跑，否则跳过并提示）
# make test-import  PostgreSQL 导入测试（必须设 VINX_TEST_PG_URL=postgresql://…/postgres，角色需能 CREATE DATABASE）
# make build-web  构建 web/ 并复制到 internal/web/dist（go:embed 嵌入）
# make build      build-web + 本机平台可执行文件 dist/vinx-vocab
# make cross      build-web + 交叉编译 dist/vinx-vocab.exe（windows/amd64，带图标与版本信息）与 dist/vinx-vocab（linux/amd64）
# make winres     生成 Windows 资源（图标、版本信息）cmd/vinx-vocab/rsrc_windows_amd64.syso（go-winres，只在 windows 构建时链接）
# make e2e        起 Go 单文件（端口 E2E_PORT / E2E_FRESH_PORT）跑 e2e/ 下的 Playwright 用例
# make contract   契约测试（CONTRACT_BASE_URL 默认 Go 本机验收端口）

# 本机私有的 Go 环境（工具链、模块缓存、代理）写在 local.mk，不入库；没有时用 PATH 里的 go 与默认环境
-include local.mk

GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
# exe 里的数字版本（x.y.z）：VERSION 形如 v1.2.3 / 1.2.3-… 时取前面的数字，否则 0.0.0；字符串版本原样写进 ProductVersion
WINVER  := $(shell echo "$(VERSION)" | sed -nE 's/^v?([0-9]+\.[0-9]+\.[0-9]+).*/\1/p' | grep . || echo 0.0.0)
GOWINRES ?= $(GO) run github.com/tc-hib/go-winres@v0.3.3
CONTRACT_BASE_URL ?= http://localhost:3200/api

export CGO_ENABLED = 0

.PHONY: test test-import vet build build-web build-go cross winres e2e contract clean

test:
	$(GO) test ./...
	@if [ -z "$$VINX_TEST_PG_URL" ]; then \
		echo ""; \
		echo "!!! 未设置 VINX_TEST_PG_URL：internal/migrate 的 PostgreSQL 导入测试已跳过（上面的 ok 不含它们）。"; \
		echo "!!! 完整运行：VINX_TEST_PG_URL=postgresql://用户:密码@主机:端口/postgres make test-import"; \
	fi

test-import:
	@if [ -z "$$VINX_TEST_PG_URL" ]; then echo "make test-import 需要 VINX_TEST_PG_URL（当前角色要能 CREATE DATABASE）" >&2; exit 1; fi
	VINX_REQUIRE_PG=1 $(GO) test -count=1 -v ./internal/migrate/

vet:
	$(GO) vet ./...

build-web:
	@if [ -f web/package.json ]; then \
		pnpm -C web install --frozen-lockfile && pnpm -C web build && \
		find internal/web/dist -mindepth 1 ! -name placeholder.html -exec rm -rf {} + && cp -r web/dist/. internal/web/dist/; \
	else \
		echo "web/ 还没有前端工程，沿用 internal/web/dist 里的占位页"; \
	fi

build-go:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/vinx-vocab ./cmd/vinx-vocab

build: build-web build-go

winres:
	cd cmd/vinx-vocab && $(GOWINRES) make --in winres/winres.json --arch amd64 --product-version "$(VERSION)" --file-version "$(WINVER)"

cross: build-web winres
	GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/vinx-vocab.exe ./cmd/vinx-vocab
	GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/vinx-vocab ./cmd/vinx-vocab

e2e: build
	bash e2e/run.sh

contract:
	BASE_URL=$(CONTRACT_BASE_URL) pnpm -C contract test

clean:
	rm -rf dist
