#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s [--history]\n' "$0" >&2
}

mode="tracked"
if [[ "${1:-}" == "--history" ]]; then
  mode="history"
  shift
fi
if [[ $# -ne 0 ]]; then
  usage
  exit 2
fi

secret_pattern='(-----BEGIN ([A-Z0-9 ]+ )?PRIVATE KEY-----|AKIA[0-9A-Z]{16}|(^|[^[:alnum:]_])(api[_-]?key|secret|token|password|passwd|auth[_-]?token)[[:space:]]*[:=][[:space:]]*["'\'' ]?[A-Za-z0-9_./+=-]{16,})'
failures=0

is_sensitive_path() {
  local path="$1"
  case "$path" in
    .env|.env.*|configs/config.yaml|local-credentials.*|credentials.local.*)
      [[ "$path" == ".env.example" ]] && return 1
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

report() {
  printf 'secret-scan: %s\n' "$1" >&2
  failures=1
}

is_allowed_fixture_line() {
  local source_path="$1"
  local text="$2"
  case "$source_path:$text" in
    *internal/mihomo/config_test.go:*super-secret-token*|*internal/mihomo/config_test.go:*test-secret*|*internal/mihomo/config_test.go:*proxy-password*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

scan_worktree_file() {
  local path="$1"
  while IFS=: read -r match_path line match_text; do
    [[ -z "${match_path:-}" ]] && continue
    is_allowed_fixture_line "$match_path" "${match_text:-}" && continue
    report "$match_path:$line: potential secret pattern"
  done < <(grep -HEni -- "$secret_pattern" "$path" 2>/dev/null || true)
}

scan_tracked() {
  local path
  while IFS= read -r path; do
    if is_sensitive_path "$path"; then
      report "tracked sensitive path: $path"
    fi
    [[ -f "$path" ]] && scan_worktree_file "$path"
  done < <(git ls-files)
}

ensure_full_history() {
  if [[ "$(git rev-parse --is-shallow-repository 2>/dev/null || printf false)" == "true" ]]; then
    printf 'secret-scan: --history requires a non-shallow clone\n' >&2
    exit 1
  fi
}

scan_history_paths() {
  local object path
  while read -r object path; do
    [[ -z "${path:-}" ]] && continue
    if is_sensitive_path "$path"; then
      report "history contains sensitive path: $path ($object)"
    fi
  done < <(git rev-list --objects --all)
}

scan_history_content() {
  local revisions
  revisions="$(git rev-list --all)"
  [[ -z "$revisions" ]] && return 0
  while IFS=: read -r commit path line match_text; do
    [[ -z "${commit:-}" ]] && continue
    is_allowed_fixture_line "$path" "${match_text:-}" && continue
    report "${commit:0:12}:$path:$line: potential secret pattern"
  done < <(git grep -IEni -- "$secret_pattern" $revisions -- 2>/dev/null || true)
}

case "$mode" in
  tracked)
    scan_tracked
    ;;
  history)
    ensure_full_history
    scan_history_paths
    scan_history_content
    ;;
  *)
    usage
    exit 2
    ;;
esac

if [[ $failures -ne 0 ]]; then
  exit 1
fi
printf 'secret scan passed (%s)\n' "$mode"
