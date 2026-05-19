#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s vTAG\n' "$0" >&2
}

if [[ $# -ne 1 ]]; then
  usage
  exit 2
fi

tag="$1"
if [[ "$tag" != v* ]]; then
  printf 'release tag must start with v: %s\n' "$tag" >&2
  exit 2
fi

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

if [[ -n "$(git status --porcelain)" ]]; then
  printf 'working tree must be clean before tagging\n' >&2
  exit 1
fi

if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  printf 'tag already exists: %s\n' "$tag" >&2
  exit 1
fi

scripts/ci-local.sh clean
git tag -a "$tag" -m "Release $tag"
printf 'Created tag %s\n' "$tag"
printf 'Push it with:\n  git push origin %s\n' "$tag"
