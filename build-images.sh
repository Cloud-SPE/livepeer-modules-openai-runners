#!/usr/bin/env bash
# Single orchestrator for the runner image set.
#
# Subcommands:
#   build [name ...]   Build all images, or a named subset. Respects base-image deps.
#   push  [name ...]   Push built images to ${REGISTRY}. REQUIRES `docker login` first.
#   validate           Run `docker compose config` against every overlay in infra/compose/.
#   clean              Remove locally-built images for ${REGISTRY}/${TAG}.
#   help               Show this help.
#
# Environment:
#   REGISTRY              Registry prefix for prod runner images (default: tztcloud).
#                         Only pushed images use this.
#   LOCAL_REGISTRY        Prefix for build-only artifacts (default: local).
#                         Bases + smoke tester are tagged here and never pushed.
#   TAG                   Image tag (default: v1.3.0)
#   PLATFORMS             Buildx platforms (default: linux/amd64). Go runners
#                         honor this; ML runners pin to linux/amd64.
#   PYTORCH_INDEX_URL     PyTorch wheel index. Default cu128 (latest cu12x).
#                         Switch to cu130 once PyTorch publishes CUDA 13 wheels.
#   CUDA_VERSION          NVIDIA CUDA tag (default: 13.2.1).
#   PYTHON_VERSION        Python version for both bases (default: 3.13).
#   GO_VERSION            Go toolchain (default: 1.25.7).
#   NODE_VERSION          Node major version for tester (default: 22).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

REGISTRY="${REGISTRY:-tztcloud}"
LOCAL_REGISTRY="${LOCAL_REGISTRY:-local}"
TAG="${TAG:-v1.3.0}"
PLATFORMS="${PLATFORMS:-linux/amd64}"
PYTORCH_INDEX_URL="${PYTORCH_INDEX_URL:-https://download.pytorch.org/whl/cu128}"
CUDA_VERSION="${CUDA_VERSION:-13.2.1}"
PYTHON_VERSION="${PYTHON_VERSION:-3.13}"
GO_VERSION="${GO_VERSION:-1.25.7}"
NODE_VERSION="${NODE_VERSION:-22}"

# Order matters: bases first, then dependents.
ALL_IMAGES=(
  python-base
  cuda13-python-base
  openai-audio-runner
  openai-tts-runner
  openai-image-generation-runner
  rerank-runner
  image-model-downloader
  rerank-model-downloader
  openai-chat-runner
  openai-embeddings-runner
  openai-tester
)

# Build-only artifacts: tagged with $LOCAL_REGISTRY and never pushed.
# - Shared bases exist solely to source the downstream multi-stage builds.
# - The Node tester is a smoke harness, not a prod runner.
LOCAL_IMAGES=(
  python-base
  cuda13-python-base
  openai-tester
)

is_local_image() {
  local n="$1"
  for x in "${LOCAL_IMAGES[@]}"; do [ "$x" = "$n" ] && return 0; done
  return 1
}

img_tag() {
  if is_local_image "$1"; then
    echo "${LOCAL_REGISTRY}/$1:${TAG}"
  else
    echo "${REGISTRY}/$1:${TAG}"
  fi
}

build_python_base() {
  local image
  image="$(img_tag python-base)"
  echo "==> Building ${image}"
  docker build \
    --build-arg "PYTHON_VERSION=${PYTHON_VERSION}" \
    -t "${image}" \
    -f infra/dockerfiles/python-base.Dockerfile \
    .
}

build_cuda13_python_base() {
  local image
  image="$(img_tag cuda13-python-base)"
  echo "==> Building ${image}"
  docker build \
    --build-arg "CUDA_VERSION=${CUDA_VERSION}" \
    --build-arg "PYTHON_VERSION=${PYTHON_VERSION}" \
    -t "${image}" \
    -f infra/dockerfiles/cuda13-python-base.Dockerfile \
    .
}

build_cuda_runner() {
  local name="$1"
  local image
  image="$(img_tag "${name}")"
  echo "==> Building ${image}"
  docker build \
    --build-arg "REGISTRY=${REGISTRY}" \
    --build-arg "TAG=${TAG}" \
    --build-arg "BASE_IMAGE=$(img_tag cuda13-python-base)" \
    --build-arg "PYTORCH_INDEX_URL=${PYTORCH_INDEX_URL}" \
    -t "${image}" \
    -f "infra/dockerfiles/${name}.Dockerfile" \
    .
}

build_cpu_runner() {
  local name="$1"
  local image
  image="$(img_tag "${name}")"
  echo "==> Building ${image}"
  docker build \
    --build-arg "REGISTRY=${REGISTRY}" \
    --build-arg "TAG=${TAG}" \
    --build-arg "BASE_IMAGE=$(img_tag python-base)" \
    -t "${image}" \
    -f "infra/dockerfiles/${name}.Dockerfile" \
    .
}

build_go_runner() {
  local name="$1"
  local image
  image="$(img_tag "${name}")"
  echo "==> Building ${image} (platforms ${PLATFORMS})"
  docker buildx build \
    --platform "${PLATFORMS}" \
    --build-arg "GO_VERSION=${GO_VERSION}" \
    -t "${image}" \
    --load \
    -f "infra/dockerfiles/${name}.Dockerfile" \
    .
}

build_tester() {
  local image
  image="$(img_tag openai-tester)"
  echo "==> Building ${image}"
  docker build \
    --build-arg "NODE_VERSION=${NODE_VERSION}" \
    -t "${image}" \
    -f infra/dockerfiles/openai-tester.Dockerfile \
    .
}

build_one() {
  case "$1" in
    python-base)                       build_python_base ;;
    cuda13-python-base)                build_cuda13_python_base ;;
    openai-audio-runner)               build_cuda_runner openai-audio-runner ;;
    openai-tts-runner)                 build_cuda_runner openai-tts-runner ;;
    openai-image-generation-runner)    build_cuda_runner openai-image-generation-runner ;;
    rerank-runner)                     build_cuda_runner rerank-runner ;;
    image-model-downloader)            build_cpu_runner image-model-downloader ;;
    rerank-model-downloader)           build_cpu_runner rerank-model-downloader ;;
    openai-chat-runner)                build_go_runner openai-chat-runner ;;
    openai-embeddings-runner)          build_go_runner openai-embeddings-runner ;;
    openai-tester)                     build_tester ;;
    *) echo "unknown image: $1" >&2; exit 2 ;;
  esac
}

build_all() {
  for name in "${ALL_IMAGES[@]}"; do
    build_one "${name}"
  done
  echo "All images built successfully."
}

push_one() {
  local name="$1"
  if is_local_image "${name}"; then
    echo "==> Skipping ${name}: local-only (never pushed)"
    return 0
  fi
  local image
  image="$(img_tag "${name}")"
  echo "==> Pushing ${image}"
  docker push "${image}"
}

cmd_build() {
  if [ "$#" -eq 0 ]; then
    build_all
  else
    for name in "$@"; do build_one "${name}"; done
  fi
}

cmd_push() {
  local targets=("$@")
  if [ "${#targets[@]}" -eq 0 ]; then
    targets=("${ALL_IMAGES[@]}")
  fi
  for name in "${targets[@]}"; do push_one "${name}"; done
}

cmd_validate() {
  echo "==> Validating compose snippets in infra/compose/"
  shopt -s nullglob
  local snippets=(infra/compose/docker-compose.*.yml)
  if [ "${#snippets[@]}" -eq 0 ]; then
    echo "no compose snippets found under infra/compose/" >&2
    exit 1
  fi
  for f in "${snippets[@]}"; do
    echo "  - $f"
    docker compose -f "$f" config >/dev/null
  done
  echo "All compose snippets valid (${#snippets[@]} file(s))."
}

cmd_clean() {
  for name in "${ALL_IMAGES[@]}"; do
    docker rmi "$(img_tag "${name}")" 2>/dev/null || true
  done
}

cmd_help() {
  sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//' | head -n -2
}

cmd="${1:-help}"
shift || true

case "${cmd}" in
  build)    cmd_build "$@" ;;
  push)     cmd_push  "$@" ;;
  validate) cmd_validate ;;
  clean)    cmd_clean ;;
  help|-h|--help) cmd_help ;;
  *) echo "unknown subcommand: ${cmd}" >&2; cmd_help; exit 2 ;;
esac
