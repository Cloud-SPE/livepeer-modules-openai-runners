#!/usr/bin/env bash
# Run every unit test in throwaway containers: go vet + go test for the two Go
# modules, python unittest for the four Python runners. No host toolchain.
#
# Env: GO_VERSION, PYTHON_VERSION from infra/build/image-versions.env.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

VERSION_ENV_FILE="${ROOT}/infra/build/image-versions.env"
if [[ -f "$VERSION_ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  . "$VERSION_ENV_FILE"
fi
GO_VERSION="${GO_VERSION:-1.25.7}"
PYTHON_VERSION="${PYTHON_VERSION:-3.13}"

GO_TEST_MODULES=(openai-chat-runner openai-embeddings-runner)
PY_TEST_PACKAGES=(openai-audio-runner openai-tts-runner openai-image-generation-runner rerank-runner)

failed=0
for mod in "${GO_TEST_MODULES[@]}"; do
  echo "==> go test ${mod}"
  docker run --rm -v "${ROOT}:/src" -w "/src/${mod}" -e GOFLAGS=-mod=mod \
    "golang:${GO_VERSION}-alpine" sh -c 'go vet ./... && go test ./...' || failed=1
done
for pkg in "${PY_TEST_PACKAGES[@]}"; do
  echo "==> python unittest ${pkg}"
  docker run --rm -v "${ROOT}:/src" -w "/src/${pkg}/src" \
    "python:${PYTHON_VERSION}-slim" python -m unittest discover -s . -p 'test_*.py' -t . || failed=1
done
if [[ "$failed" -ne 0 ]]; then
  echo "tests failed" >&2
  exit 1
fi
echo "All tests passed."
