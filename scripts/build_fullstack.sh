#!/usr/bin/env sh
set -eu
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_dir/app"
command -v go >/dev/null
command -v pnpm >/dev/null
(
  cd frontend/admin
  pnpm install --frozen-lockfile
  pnpm run build:fullstack
)
(
  cd frontend/user
  pnpm install --frozen-lockfile
  pnpm run build
)
rm -rf internal/web/dist
mkdir -p internal/web/dist
cp -R frontend/admin/dist internal/web/dist/admin
cp -R frontend/user/dist internal/web/dist/user
mkdir -p "$repo_dir/bin"
CGO_ENABLED=0 go build -trimpath -tags release,fullstack \
  -ldflags='-s -w -X github.com/dujiao-next/internal/version.Version=v1.4.8-evan.20261005 -X github.com/dujiao-next/internal/version.BuildType=source' \
  -o "$repo_dir/bin/dujiao-next" ./cmd/server
printf '%s\n' "构建完成：$repo_dir/bin/dujiao-next"
