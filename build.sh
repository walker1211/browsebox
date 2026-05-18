#!/usr/bin/env bash
set -euo pipefail

command="${1:-build}"
prefix="${PREFIX:-/usr/local}"

build() {
  go test ./...
  go build -o browsebox ./cmd/browsebox
  go build -o skill-sync ./cmd/skill-sync
}

case "$command" in
  build)
    build
    ;;
  install)
    build
    install -d "$prefix/bin"
    install -m 0755 browsebox "$prefix/bin/browsebox"
    install -m 0755 skill-sync "$prefix/bin/browsebox-skill-sync"
    ;;
  *)
    printf 'usage: %s [build|install]\n' "$0" >&2
    exit 2
    ;;
esac
