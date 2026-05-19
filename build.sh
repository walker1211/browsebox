#!/usr/bin/env bash
set -euo pipefail

command="${1:-build}"
prefix="${PREFIX:-/usr/local}"

build() {
  printf 'Building...\n'
  go test ./...
  go build -o browsebox ./cmd/browsebox
  go build -o skill-sync ./cmd/skill-sync
  printf 'Done. Binaries: ./browsebox ./skill-sync\n'
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
    printf 'Installed: %s/bin/browsebox %s/bin/browsebox-skill-sync\n' "$prefix" "$prefix"
    ;;
  *)
    printf 'usage: %s [build|install]\n' "$0" >&2
    exit 2
    ;;
esac
