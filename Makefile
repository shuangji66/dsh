# Makefile: builds the Vue frontend, embeds the static assets into the Go
# backend binary, then compiles the single self-contained harness backend.
#
#   make              # dev (default, non-stripped)
#   make release V=1.0.1   # release build (strip + -ldflags), version override
#   make clean        # remove build artifacts
#
# The backend serves the embedded assets over its unix socket under the
# configured baseurl (e.g. /app/Harness) fronted by nginx.
#
# Go cache/module variables default to project-local dirs (same as build.sh);
# pre-set environment values (GOCACHE/GOPATH) still take precedence.

ROOT      := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
BACKEND   := $(ROOT)/backend
FRONTEND  := $(ROOT)/frontend
EMBED     := $(BACKEND)/embed
OUT       := $(BACKEND)/harness

# 控制台版本号：默认 1.0.0，可经 V 覆盖（如 make release V=1.0.1）。
version   ?= 1.0.0
V         ?= $(version)

# 与 build.sh 一致：默认把 Go 缓存放到项目本地目录；若环境中已设置
# GOCACHE/GOPATH 则沿用环境值（?= 只在未定义时赋值）。
GOCACHE   ?= $(ROOT)/.gocache
GOPATH    ?= $(ROOT)/.gopath
export GOCACHE GOPATH
export GOFLAGS="-buildvcs=false"

.PHONY: all dev release clean

all: dev

# Frontend build artifacts / node_modules flagged in ROOT/.gitignore.
.NOTPARALLEL:

dev: ## Dev build (default, non-stripped)
	$(MAKE) build LDFLAGS="-X main.harnessVersion=$(V)"

release: ## Release build (strip + external linking)
	$(MAKE) build LDFLAGS="-s -w -linkmode=external -X main.harnessVersion=$(V)"

# Common build steps. LDFLAGS is passed in from dev/release.
build:
	@echo "==> Building frontend..."
	cd "$(FRONTEND)" && npm install --no-audit --no-fund && npm run build
	@echo "==> Copying frontend dist into embed dir..."
	rm -rf "$(EMBED)"
	mkdir -p "$(EMBED)"
	cp -r "$(FRONTEND)"/dist/* "$(EMBED)"/
	@echo "==> Building Go binary..."
	# Ensure local Go cache dirs exist (build.sh 同样如此)。
	mkdir -p "$(GOCACHE)" "$(GOPATH)"
	# Clean the Go build cache so code and embedded-asset changes always compile in.
	go clean -cache
	cd "$(BACKEND)" && go build -trimpath -ldflags "$(LDFLAGS)" -o "$(OUT)" .
	@echo "==> Cleaning embed dir..."
	rm -rf "$(EMBED)"
	@echo "==> Done: $(OUT)"
	ls -lh "$(OUT)"

clean:
	rm -rf "$(EMBED)"
	rm -f "$(BACKEND)/harness"