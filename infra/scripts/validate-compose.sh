#!/usr/bin/env bash
# Run `docker compose config` against every overlay in infra/compose/.
# A compose file that does not parse is a deploy that fails at 2am.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

shopt -s nullglob
snippets=(infra/compose/docker-compose.*.yml)
if [[ ${#snippets[@]} -eq 0 ]]; then
  echo "no compose snippets found under infra/compose/" >&2
  exit 1
fi
echo "==> Validating ${#snippets[@]} compose overlay(s)"
for f in "${snippets[@]}"; do
  echo "  - $f"
  docker compose -f "$f" config >/dev/null
done
echo "All compose snippets valid."
