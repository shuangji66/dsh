#!/bin/bash
# build-noterminal.sh — 交叉编译*不含终端功能*的控制台二进制（linux/amd64 + linux/arm64）。
#
#   ./build-noterminal.sh              # 产物：./dist/harness-amd64、./dist/harness-arm64
#   ./build-noterminal.sh /tmp/out     # 指定输出目录
#   VERSION=1.0.1 ./build-noterminal.sh
#
# 与 build.sh / Makefile 的 TERMINAL=0 是同一条链路，只是把两个架构一次编完：
#   - 前端 VITE_TERMINAL=0：TerminalView 被占位页顶替，xterm 与 xterm.css 不进包；
#   - Go -tags noterminal：PTY 会话、/terminal WebSocket、/api/sessions*、
#     快捷指令接口整体剔除（见 backend/terminal_disabled.go）。
# 两条链路都不依赖 cgo，因此固定 CGO_ENABLED=0 交叉编译。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND="$ROOT/backend"
FRONTEND="$ROOT/frontend"
EMBED="$BACKEND/embed"
OUT_DIR="${1:-$ROOT/dist}"
VERSION="${VERSION:-1.0.0}"
GO_TAGS="noterminal"
TARGET_ARCHES=(amd64 arm64)

export PATH=/var/apps/nodejs_v24/target/bin:$PATH
export GOFLAGS="-buildvcs=false"
export GOCACHE="${GOCACHE:-$ROOT/.gocache}"
export GOPATH="${GOPATH:-$ROOT/.gopath}"
export TERMINAL=0
export VITE_TERMINAL=0
mkdir -p "$GOCACHE" "$GOPATH" "$OUT_DIR"

# embed 是前端产物的暂存目录（//go:embed all:embed）：构建结束或失败都清掉。
cleanup() { rm -rf "$EMBED"; }
trap cleanup EXIT

echo "==> Building frontend (no terminal)..."
( cd "$FRONTEND" && npm install --no-audit --no-fund && VITE_TERMINAL="$VITE_TERMINAL" npm run build )

echo "==> Verifying no terminal assets in dist..."
if grep -rIl --include='*.js' --include='*.css' --include='*.html' 'xterm' "$FRONTEND/dist"; then
  echo "ERROR: xterm leaked into the frontend bundle (TERMINAL=0 build)" >&2
  exit 1
fi

echo "==> Staging frontend dist into backend/embed..."
rm -rf "$EMBED"
mkdir -p "$EMBED"
cp -r "$FRONTEND"/dist/* "$EMBED"/

echo "==> Building Go binaries (tags: $GO_TAGS, version: $VERSION)..."
# 清缓存，保证刚换进去的 embed 资产一定被编进二进制（与 Makefile / build.sh 一致）。
go clean -cache
for arch in "${TARGET_ARCHES[@]}"; do
  echo "    linux/$arch -> $OUT_DIR/harness-$arch"
  ( cd "$BACKEND" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
      go build -trimpath -tags "$GO_TAGS" \
      -ldflags "-s -w -X main.harnessVersion=$VERSION" \
      -o "$OUT_DIR/harness-$arch" . )
done

echo "==> Done:"
ls -lh "$OUT_DIR"/harness-amd64 "$OUT_DIR"/harness-arm64
sha256sum "$OUT_DIR"/harness-amd64 "$OUT_DIR"/harness-arm64
