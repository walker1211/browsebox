#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s [clean]\n' "$0" >&2
}

mode="${1:-current}"
if [[ $# -gt 1 || "$mode" != "current" && "$mode" != "clean" ]]; then
  usage
  exit 2
fi

repo_root="$(git rev-parse --show-toplevel)"

gofmt_check() {
  local unformatted
  unformatted="$(git ls-files -z '*.go' | xargs -0 gofmt -l)"
  if [[ -n "$unformatted" ]]; then
    printf 'gofmt needed:\n%s\n' "$unformatted" >&2
    return 1
  fi
}

verify_binaries() {
  [[ -x ./browsebox ]] || { printf 'missing executable: ./browsebox\n' >&2; return 1; }
  [[ -x ./skill-sync ]] || { printf 'missing executable: ./skill-sync\n' >&2; return 1; }
}

run_checks() {
  ./scripts/secret-scan.sh
  gofmt_check
  go vet ./...
  go test ./...
  ./build.sh
  verify_binaries
}

copy_tracked_tree() {
  local destination="$1"
  local path target_dir
  while IFS= read -r -d '' path; do
    target_dir="$destination/$(dirname "$path")"
    mkdir -p "$target_dir"
    cp -p "$repo_root/$path" "$destination/$path"
  done < <(cd "$repo_root" && git ls-files -z)
}

if [[ "$mode" == "clean" ]]; then
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  copy_tracked_tree "$tmp"
  cd "$tmp"
  git init -q
  git add .
  run_checks
  exit 0
fi

cd "$repo_root"
run_checks
