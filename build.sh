#!/bin/bash
# Build script: builds the Vue frontend and embeds the static assets into the
# Go backend binary, then compiles the single self-contained harness-backend.
#
#   ./build.sh            # dev (default, non-stripped)
#   ./build.sh release    # release build (strip + -ldflags)
#
# The backend serves these embedded assets over its unix socket under the
# configured baseurl (e.g. /app/Harness) fronted by nginx.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_DIR="${ROOT}/app/backend"
FRONTEND_DIR="${ROOT}/app/frontend"
EMBED_DIR="${BACKEND_DIR}/embed"

export GOCACHE="${GOCACHE:-${ROOT}/.gocache}"
export GOPATH="${GOPATH:-${ROOT}/.gopath}"
export PATH=/var/apps/nodejs_v24/target/bin:$PATH
mkdir -p "$GOCACHE" "$GOPATH"

echo "==> Building frontend..."
# The baseurl is handled at runtime by the backend (not baked into the binary),
# so build with vite's default relative base (./assets/...).
( cd "$FRONTEND_DIR" && npm install --no-audit --no-fund && npm run build )

echo "==> Copying frontend dist into embed dir..."
rm -rf "$EMBED_DIR"
mkdir -p "$EMBED_DIR"
cp -r "$FRONTEND_DIR"/dist/* "$EMBED_DIR"/

echo "==> Building Go binary..."
# 控制台版本号：默认 1.0.0，可经第二个参数覆盖（如 ./build.sh release 1.0.1）。
HARNESS_VERSION="${2:-1.0.0}"
LDFLAGS="-s -w -X main.harnessVersion=${HARNESS_VERSION}"
OUT="${ROOT}/app/backend/harness"
if [ "${1:-}" = "release" ]; then
  LDFLAGS="-s -w -linkmode=external -X main.harnessVersion=${HARNESS_VERSION}"
fi
# Clean the Go build cache so code and embedded-asset changes always compile in.
go clean -cache
( cd "$BACKEND_DIR" && go build -trimpath -ldflags "$LDFLAGS" -o "$OUT" . )

echo "==> Cleaning embed dir..."
rm -rf "$EMBED_DIR"

echo "==> Done: ${OUT}"
ls -lh "$OUT"