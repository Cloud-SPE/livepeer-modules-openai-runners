#!/usr/bin/env bash
# Real HTTP handler checks using installed runtime dependencies, without models/GPU.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
if [[ $# -eq 0 ]]; then
  set -- openai-audio-runner openai-tts-runner openai-image-generation-runner rerank-runner
fi
for name in "$@"; do
  docker run --rm --network none -e DEVICE=cpu -e MODEL_ID=test \
    -e PYTHONPATH="/src/${name}/src" -v "${ROOT}:/src:ro" \
    "${REGISTRY:-tztcloud}/${name}:${TAG:-v2.0.0}" \
    python /src/infra/scripts/check-capacity.py "${name//-/_}"
done
