# CORE-BELIEFS

The opinionated, mechanical rules that bind every change in this repo. Read
before making load-bearing decisions.

## 1. Docker-first

Every gesture is a `docker` invocation. There is no host Python, host Go, or
host Node toolchain step. If you're tempted to add one, write a Dockerfile
stage instead.

## 2. One image per capability

Each Docker image declares exactly one `CAPABILITY_NAME`. Don't multiplex
capabilities into a single image. The `openai-audio-runner` is the only
exception — it serves two related capabilities
(`openai-audio-transcriptions` + `openai-audio-translations`) from the same
Whisper model load, but operates as one image.

## 3. Capability identity is image-tag-pinned

The image tag and the capability identity move together. Bumping a model
version is a new tag. Changing the capability name is a new image.

## 4. GPU probe fails fast

ML runners exit non-zero at startup if `DEVICE=cuda` and no GPU is detected.
Loud failure beats quiet degradation. Operators choose `DEVICE=cpu` to fall back.

## 5. Metrics are opt-in

`METRICS_ENABLED=false` by default. `METRICS_ENABLED=true` exposes `/metrics`.
Default-off means zero overhead for stacks that don't scrape.

## 6. Multi-arch policy

- **Go runners** (chat, embeddings) — ship `linux/amd64` + `linux/arm64`.
- **ML runners** + Python downloaders + tester — ship `linux/amd64` only.
  CUDA + diffusers/transformers stacks don't support arm64 reliably.

## 7. Runners are blind to customer identity

No auth, billing, or payment validation inside a runner. The broker
authenticates upstream; the runner only sees fully-paid HTTP requests at its
declared endpoint plus two informational headers (`Livepeer-Capability`,
`Livepeer-Offering`). See [`TRUST-MODEL.md`](./TRUST-MODEL.md).

## 8. No per-runner LICENSE files

The repo-root [`LICENSE`](./LICENSE) applies to everything. Per-subdir LICENSE
files create drift.

## 9. Build context = repo root

Every Dockerfile in `infra/dockerfiles/` is built with the repo root as build
context. This is what lets each Dockerfile COPY both its runner's source and
the offering manifest from `infra/offerings/`.

## 10. Default tag is `v1.3.0`

Keep shared bases and downstream runner builds on the same tag unless the
caller overrides `TAG=...`. Mixed tags between a base and its consumer is a
near-certain bug.

## 11. PyTorch wheels follow CUDA forward-compat

Today: CUDA 13 runtime base + PyTorch `cu128` wheels (latest CUDA 12.x). Once
PyTorch publishes `cu130` wheels, flip `PYTORCH_INDEX_URL`. See
[`PLANS.md`](./PLANS.md).

## 12. uv for Python dependency management

Replaces pip in every Python image. Faster installs; can manage standalone
Python builds in CUDA images; smaller surface area than pip + virtualenv
combined.
