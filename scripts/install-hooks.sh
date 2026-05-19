#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
hook_path="$(git rev-parse --git-path hooks/pre-push)"

if [[ -e "$hook_path" ]]; then
  printf 'pre-push hook already exists: %s\n' "$hook_path" >&2
  printf 'Move it aside or merge this command manually:\n' >&2
  printf '  scripts/ci-local.sh clean\n' >&2
  exit 1
fi

mkdir -p "$(dirname "$hook_path")"
cat >"$hook_path" <<'HOOK'
#!/usr/bin/env bash
set -euo pipefail
repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"
scripts/ci-local.sh clean
HOOK
chmod 0755 "$hook_path"
printf 'Installed pre-push hook: %s\n' "$hook_path"
printf 'It runs scripts/ci-local.sh clean before push.\n'
