# shr Makefile — 统一构建入口，产物一律输出到 dist/
BINARY   := shr
MODULE   := github.com/havoc-rao/shell-rewrite
CMD      := ./cmd/shr
DIST     := dist
VERSION  := $(shell tr -d '[:space:]' < version/VERSION)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE := $(shell date -u +%Y-%m-%d)

LDFLAGS  := -s -w \
	-X $(MODULE)/cli.Commit=$(GIT_COMMIT) \
	-X $(MODULE)/cli.Date=$(BUILD_DATE)

.PHONY: build run release snapshot clean install test test-verbose test-unit test-tests help

## build: 编译当前平台二进制到 dist/shr
build:
	mkdir -p $(DIST)
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) $(CMD)

## run: 编译并立即运行（透传参数: make run args="version"）
run: build
	./$(DIST)/$(BINARY) $(args)

## release: 发版。用法:
##   make release          自动 patch+1 (0.1.0 → 0.1.1)
##   make release v=0.2.0  指定版本号 (升级 minor/major 时用)
##   写入 version/VERSION → commit → push，CI 自动打 tag 并跑 goreleaser
release:
	@cur=$$(tr -d '[:space:]' < version/VERSION); \
	if [ -n "$(v)" ]; then new="$(v)"; \
	else \
		major=$$(echo $$cur | cut -d. -f1); \
		minor=$$(echo $$cur | cut -d. -f2); \
		patch=$$(echo $$cur | cut -d. -f3); \
		patch=$$((patch + 1)); \
		new="$$major.$$minor.$$patch"; \
	fi; \
	echo "$$new" > version/VERSION; \
	git add version/VERSION; \
	git commit -m "release: v$$new"; \
	git push origin $$(git rev-parse --abbrev-ref HEAD); \
	echo "✓ v$$cur → v$$new 已推送，CI 将自动打 tag 并发布 Release"

## snapshot: 本地跑 goreleaser 快照构建（不发版，产物在 dist/）
snapshot:
	goreleaser release --snapshot --clean

## install: 编译并安装到 /usr/local/bin（先 build 再 cp，共用 dist/shr 产物）
install: build
	cp $(DIST)/$(BINARY) /usr/local/bin/$(BINARY)
	@echo "✓ 已安装到 /usr/local/bin/$(BINARY)"

## test: 运行全部测试（core + tests/，禁用缓存避免误报）
test:
	go test ./... -count=1

## clean: 清理 dist/
clean:
	rm -rf $(DIST)

## help: 显示本帮助
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //; s/: /\t/'
