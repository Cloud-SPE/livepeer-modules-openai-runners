#!/usr/bin/env bash
# Build the runner image set in dependency order.
#
# Usage:
#   ./infra/scripts/build-images.sh                  # build everything
#   ./infra/scripts/build-images.sh chat rerank      # a subset (substring match on the
#                                                    # image name); base images a selected
#                                                    # image needs are added automatically
#   ./infra/scripts/build-images.sh --list [filter]  # print the full tags, build nothing
#
# Env:
#   REGISTRY           default: tztcloud. Prefix for the images that get pushed.
#   LOCAL_REGISTRY     default: local. Prefix for build-only images (the two shared
#                      bases and the smoke tester); these are never pushed.
#   TAG                default: IMAGE_TAG_DEFAULT from infra/build/image-versions.env
#   VERSION            default: derived from git (exact tag, else TAG-<sha>, with a
#                      -dirty suffix on uncommitted work). Stamped into the Go binaries
#                      and every image's org.opencontainers.image.version label.
#   PUSH               set to 1 to docker push after each build.
#   PLATFORMS          buildx platforms for the Go runners (default: linux/amd64).
#                      More than one platform needs PUSH=1: a manifest list cannot
#                      be loaded into the local daemon.
#   PYTORCH_INDEX_URL  PyTorch wheel index for the CUDA runners. Default cu128
#                      (sm_75+). For a GTX 1080-class card build the -pascal flavor:
#                        TAG=v2.0.0-pascal PYTORCH_INDEX_URL=https://download.pytorch.org/whl/cu126 \
#                          ./infra/scripts/build-images.sh audio tts rerank-runner
#                      (no image-generation variant: FLUX does not fit an 8 GB card)
#   GO_VERSION, NODE_VERSION, PYTHON_VERSION, ALPINE_VERSION, UBUNTU_VERSION,
#   CUDA_VERSION       toolchain pins, from infra/build/image-versions.env.
#
# Pushing is held to a stricter standard than building. A local build can be
# thrown away; a pushed tag is what somebody else deploys. So PUSH=1 refuses a
# dirty tree — an image built from uncommitted work cannot be rebuilt from any
# commit — and it prints the digest of everything it pushed and records them in
# infra/build/<TAG>-digests.txt. Pin the digest, not the tag: TAG defaults to a
# constant, so `v2.0.0` means "whatever was pushed last", while a digest means
# one specific image forever.
#
# Notes:
#   - Build context is always the repo root; every Dockerfile expects that.
#   - Same pattern as livepeer-network-modules/infra/scripts/build-images.sh,
#     plus the base-image dependency and buildx handling this repo needs.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

VERSION_ENV_FILE="${ROOT}/infra/build/image-versions.env"
if [[ -f "$VERSION_ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  . "$VERSION_ENV_FILE"
fi

REGISTRY="${REGISTRY:-tztcloud}"
LOCAL_REGISTRY="${LOCAL_REGISTRY:-local}"
TAG="${TAG:-${IMAGE_TAG_DEFAULT:-v2.0.0}}"
PUSH="${PUSH:-0}"
PLATFORMS="${PLATFORMS:-linux/amd64}"
PYTORCH_INDEX_URL="${PYTORCH_INDEX_URL:-${PYTORCH_INDEX_URL_DEFAULT:-https://download.pytorch.org/whl/cu128}}"
DEFAULT_VERSION="$(VERSION_PREFIX="${TAG}" FALLBACK_VERSION="${TAG}" ./infra/build/git-version.sh)"
VERSION="${VERSION:-${DEFAULT_VERSION}}"
DIGEST_FILE="${ROOT}/infra/build/${TAG}-digests.txt"

# ---- helpers --------------------------------------------------------------

log()  { printf '\033[1;34m[build]\033[0m %s\n' "$*" >&2; }
ok()   { printf '\033[1;32m[ ok ]\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*" >&2; }
fail() { printf '\033[1;31m[fail]\033[0m %s\n' "$*" >&2; exit 1; }

# A dirty tree is fine to build from and not fine to publish from.
if [[ "$PUSH" == "1" && "$VERSION" == *-dirty ]]; then
  printf '\033[1;31m[fail]\033[0m refusing to push: working tree has uncommitted changes\n' >&2
  printf '       version would be %s, which no commit can reproduce.\n' "$VERSION" >&2
  git status --short >&2
  exit 1
fi
if [[ "$PUSH" != "1" && "$PLATFORMS" == *,* ]]; then
  fail "PLATFORMS=${PLATFORMS} needs PUSH=1: a multi-platform image cannot be loaded into the local daemon"
fi

# Build-only images: tagged under LOCAL_REGISTRY, never pushed.
is_pushable_image() {
  case "$1" in
    openai-audio-runner|\
    openai-tts-runner|\
    openai-image-generation-runner|\
    rerank-runner|\
    image-model-downloader|\
    rerank-model-downloader|\
    openai-chat-runner|\
    openai-embeddings-runner)
      return 0 ;;
    *)
      return 1 ;;
  esac
}

img_tag() {
  if is_pushable_image "$1"; then
    echo "${REGISTRY}/$1:${TAG}"
  else
    echo "${LOCAL_REGISTRY}/$1:${TAG}"
  fi
}

# Each entry: "name|dockerfile|builder|needs"
#   builder  docker  — plain docker build for the host platform
#            buildx  — Go runners: --platform PLATFORMS, --push or --load
#   needs    a base image that must be built first at the same TAG; it is
#            passed to the Dockerfile as BASE_IMAGE.
# Order matters: bases before their dependents.
declare -a IMAGES=(
  "python-base|infra/dockerfiles/python-base.Dockerfile|docker|"
  "cuda13-python-base|infra/dockerfiles/cuda13-python-base.Dockerfile|docker|"
  "openai-audio-runner|infra/dockerfiles/openai-audio-runner.Dockerfile|docker|cuda13-python-base"
  "openai-tts-runner|infra/dockerfiles/openai-tts-runner.Dockerfile|docker|cuda13-python-base"
  "openai-image-generation-runner|infra/dockerfiles/openai-image-generation-runner.Dockerfile|docker|cuda13-python-base"
  "rerank-runner|infra/dockerfiles/rerank-runner.Dockerfile|docker|cuda13-python-base"
  "image-model-downloader|infra/dockerfiles/image-model-downloader.Dockerfile|docker|python-base"
  "rerank-model-downloader|infra/dockerfiles/rerank-model-downloader.Dockerfile|docker|python-base"
  "openai-chat-runner|infra/dockerfiles/openai-chat-runner.Dockerfile|buildx|"
  "openai-embeddings-runner|infra/dockerfiles/openai-embeddings-runner.Dockerfile|buildx|"
  "openai-tester|infra/dockerfiles/openai-tester.Dockerfile|docker|"
)

GLOBAL_BUILD_ARGS=(
  "--build-arg=REGISTRY=${REGISTRY}"
  "--build-arg=LOCAL_REGISTRY=${LOCAL_REGISTRY}"
  "--build-arg=TAG=${TAG}"
  "--build-arg=VERSION=${VERSION}"
  "--build-arg=GO_VERSION=${GO_VERSION:-1.25.7}"
  "--build-arg=NODE_VERSION=${NODE_VERSION:-22}"
  "--build-arg=PYTHON_VERSION=${PYTHON_VERSION:-3.13}"
  "--build-arg=ALPINE_VERSION=${ALPINE_VERSION:-3.20}"
  "--build-arg=UBUNTU_VERSION=${UBUNTU_VERSION:-24.04}"
  "--build-arg=CUDA_VERSION=${CUDA_VERSION:-13.2.1}"
  "--build-arg=PYTORCH_INDEX_URL=${PYTORCH_INDEX_URL}"
)

# ---- filter + dependency resolution ---------------------------------------

LIST_ONLY=0
declare -a filter_args=()
for a in "$@"; do
  case "$a" in
    --list) LIST_ONLY=1 ;;
    -h|--help) sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//' | head -n -2; exit 0 ;;
    *) filter_args+=("$a") ;;
  esac
done

declare -A WANT=()
if [[ ${#filter_args[@]} -eq 0 ]]; then
  for entry in "${IMAGES[@]}"; do WANT["${entry%%|*}"]=1; done
else
  for entry in "${IMAGES[@]}"; do
    name="${entry%%|*}"
    for f in "${filter_args[@]}"; do
      if [[ "$name" == *"$f"* ]]; then WANT["$name"]=1; break; fi
    done
  done
  if [[ ${#WANT[@]} -eq 0 ]]; then
    fail "No images matched filter(s): ${filter_args[*]}"
  fi
  # Pull in the base each selected image needs.
  for entry in "${IMAGES[@]}"; do
    IFS='|' read -r name _ _ needs <<<"$entry"
    if [[ -n "${WANT[$name]:-}" && -n "$needs" && -z "${WANT[$needs]:-}" ]]; then
      WANT["$needs"]=1
      log "adding ${needs}: ${name} builds on it"
    fi
  done
fi

declare -a SELECTED=()
for entry in "${IMAGES[@]}"; do
  [[ -n "${WANT[${entry%%|*}]:-}" ]] && SELECTED+=("$entry")
done
total=${#SELECTED[@]}

if [[ "$LIST_ONLY" == "1" ]]; then
  for entry in "${SELECTED[@]}"; do img_tag "${entry%%|*}"; done
  exit 0
fi

# ---- build loop -----------------------------------------------------------

log "registry=${REGISTRY}  tag=${TAG}  version=${VERSION}  push=${PUSH}  platforms=${PLATFORMS}  building ${total} image(s)"

step=0
declare -a PUSHED_DIGESTS=()

for entry in "${SELECTED[@]}"; do
  step=$((step + 1))
  IFS='|' read -r name dockerfile builder needs <<<"$entry"
  full_tag="$(img_tag "$name")"
  pushing=0
  if [[ "$PUSH" == "1" ]] && is_pushable_image "$name"; then pushing=1; fi

  args=(-t "$full_tag" -f "$dockerfile" "${GLOBAL_BUILD_ARGS[@]}")
  [[ -n "$needs" ]] && args+=("--build-arg=BASE_IMAGE=$(img_tag "$needs")")

  log "[$step/$total] $full_tag"
  case "$builder" in
    docker)
      docker build "${args[@]}" . || fail "build failed for $full_tag"
      if [[ "$pushing" == "1" ]]; then
        log "[$step/$total] pushing $full_tag"
        docker push "$full_tag" || fail "push failed for $full_tag"
      fi
      ;;
    buildx)
      if [[ "$pushing" == "1" ]]; then
        docker buildx build --platform "$PLATFORMS" "${args[@]}" --push . || fail "build+push failed for $full_tag"
      else
        docker buildx build --platform "$PLATFORMS" "${args[@]}" --load . || fail "build failed for $full_tag"
      fi
      ;;
    *) fail "unknown builder '$builder' for $name" ;;
  esac
  ok "[$step/$total] $full_tag"

  if [[ "$PUSH" == "1" && "$pushing" != "1" ]]; then
    ok "[$step/$total] local-only image; not pushed"
  elif [[ "$pushing" == "1" ]]; then
    digest="$(docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$full_tag" 2>/dev/null || true)"
    PUSHED_DIGESTS+=("${name}|${REGISTRY}/${name}@${digest:-digest-unavailable}")
    ok "[$step/$total] pushed $full_tag"
  fi
done

ok "all $total image(s) built (registry=${REGISTRY} tag=${TAG} version=${VERSION})"

if [[ "$PUSH" == "1" && ${#PUSHED_DIGESTS[@]} -gt 0 ]]; then
  pushed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  digest_file_rel="${DIGEST_FILE#"$ROOT"/}"
  {
    echo "# ${TAG} - pushed ${pushed_at} from ${VERSION}"
    for entry in "${PUSHED_DIGESTS[@]}"; do
      printf '%-32s %s\n' "${entry%%|*}" "${entry#*|}"
    done
  } >> "$DIGEST_FILE"
  echo
  echo "Pin these digests in the deployment - a tag can be moved, a digest cannot."
  echo "Also appended to ${digest_file_rel}; check it in with the release:"
  for entry in "${PUSHED_DIGESTS[@]}"; do
    printf '  %-32s %s\n' "${entry%%|*}" "${entry#*|}"
  done
fi
