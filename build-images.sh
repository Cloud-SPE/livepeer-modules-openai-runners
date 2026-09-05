#!/usr/bin/env bash
# Compatibility shim. The build system lives under infra/:
#   infra/scripts/build-images.sh      build (PUSH=1 to push)   — same pattern as
#                                      livepeer-network-modules/infra/scripts/build-images.sh
#   infra/scripts/validate-compose.sh  docker compose config on every overlay
#   infra/scripts/test.sh              go + python unit tests in Docker
#   infra/build/image-versions.env     default TAG and toolchain pins
#
# Old subcommands still work:
#   ./build-images.sh build [name ...]   ./build-images.sh push [name ...]
#   ./build-images.sh validate           ./build-images.sh test
#   ./build-images.sh clean
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cmd="${1:-help}"
shift || true
case "$cmd" in
  build)    exec "$ROOT/infra/scripts/build-images.sh" "$@" ;;
  push)     PUSH=1 exec "$ROOT/infra/scripts/build-images.sh" "$@" ;;
  validate) exec "$ROOT/infra/scripts/validate-compose.sh" ;;
  test)     exec "$ROOT/infra/scripts/test.sh" ;;
  clean)    "$ROOT/infra/scripts/build-images.sh" --list "$@" | xargs -r -n1 docker rmi 2>/dev/null || true ;;
  help|-h|--help) sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//' | head -n -2 ;;
  *) echo "unknown subcommand: $cmd" >&2; exit 2 ;;
esac
