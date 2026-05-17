#!/usr/bin/env bash
set -euo pipefail

command="${1:-build}"
prefix="${PREFIX:-/usr/local}"

build() {
  go test ./...
  go build -o browsebox ./cmd/browsebox
}

case "$command" in
  build)
    build
    ;;
  install)
    build
    install -d "$prefix/bin"
    install -m 0755 browsebox "$prefix/bin/browsebox"
    ;;
  *)
    printf 'usage: %s [build|install]\n' "$0" >&2
    exit 2
    ;;
esac
