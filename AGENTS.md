# AGENTS.md

This repo is the standalone home for the OpenAI-shaped and Cohere-compatible
runners that the Livepeer capability broker forwards paid jobs to.

This file is a **map**, not a manual. Start here, then navigate to the deeper
sources of truth listed below.

## Operating principles

Inherited from the agent-first harness pattern in
[`docs/references/openai-harness-engineer.md`](./docs/references/openai-harness-engineer.md):

- **You steer; the agent executes.** Humans set intent; tools and feedback loops do the rest.
- **The repo is the system of record.** If it isn't checked in, it doesn't exist.
- **Progressive disclosure.** This file is a *map*, not a manual.
- **Enforce invariants, not implementations.** Constraints in lints/CI; choices in code.
- **Throughput over ceremony.** Short-lived PRs; fix-forward over block.

Component-specific principles (read [`CORE-BELIEFS.md`](./CORE-BELIEFS.md) before
load-bearing decisions):

- **Runners are blind to customer identity.** No auth, billing, or payment
  validation inside the runner.
- **Capability identity is image-tag-pinned.** One `CAPABILITY_NAME` per image,
  declared by the runner itself at `GET /.well-known/livepeer-runner`.
- **GPU probe fails fast.** ML runners exit non-zero if `DEVICE=cuda` and no GPU.
- **Metrics are opt-in.** `METRICS_ENABLED=true` exposes `/metrics`.
- **Multi-arch policy.** ML runners ship amd64-only; Go runners ship amd64+arm64.
- **Docker-first.** Every gesture is a docker invocation; no host toolchain.

## Where to look

| Question | File |
|---|---|
| What is this repo? | [`README.md`](./README.md) |
| Top-level domain map | [`ARCHITECTURE.md`](./ARCHITECTURE.md) |
| One-page mental model | [`DESIGN.md`](./DESIGN.md) |
| What's on the roadmap? | [`PLANS.md`](./PLANS.md) |
| Who consumes these runners? | [`PRODUCT_SENSE.md`](./PRODUCT_SENSE.md) |
| Per-runner grade + gaps | [`QUALITY_SCORE.md`](./QUALITY_SCORE.md) |
| Reliability invariants | [`RELIABILITY.md`](./RELIABILITY.md) |
| Trust + security model | [`SECURITY.md`](./SECURITY.md) |
| Core beliefs binding any change | [`CORE-BELIEFS.md`](./CORE-BELIEFS.md) |
| Broker ↔ runner HTTP contract | [`BROKER-CONTRACT.md`](./BROKER-CONTRACT.md) |
| What runners see vs. what broker handles | [`TRUST-MODEL.md`](./TRUST-MODEL.md) |
| Allowed `CAPABILITY_NAME` values | [`CANONICAL-CAPABILITIES.md`](./CANONICAL-CAPABILITIES.md) |
| Shared base images + inheritance | [`SHARED-BASE-IMAGES.md`](./SHARED-BASE-IMAGES.md) |
| Per-runner HTTP surface invariants | [`RUNNER-INVARIANTS.md`](./RUNNER-INVARIANTS.md) |
| Per-runner details + operator runbooks | [`RUNNERS.md`](./RUNNERS.md) |
| Release history | [`CHANGELOG.md`](./CHANGELOG.md) |
| The harness pattern that shapes this repo | [`docs/references/openai-harness-engineer.md`](./docs/references/openai-harness-engineer.md) |

## Components

Eight runner images plus two shared bases. Source under each runner's subdir;
Dockerfiles under [`infra/dockerfiles/`](./infra/dockerfiles/); compose overlays
under [`infra/compose/`](./infra/compose/); offering manifests under
[`infra/offerings/`](./infra/offerings/); env templates under
[`infra/env/`](./infra/env/).

- `openai-chat-runner/` — Go proxy for chat completions (vLLM / Ollama upstream).
- `openai-embeddings-runner/` — Go proxy for text embeddings (vLLM / Ollama upstream).
- `openai-audio-runner/` — Python (FastAPI) Whisper STT.
- `openai-tts-runner/` — Python (FastAPI) Kokoro TTS.
- `openai-image-generation-runner/` — Python (FastAPI) diffusers image gen.
- `rerank-runner/` — Python (FastAPI) Cohere-compatible CrossEncoder.
- `image-model-downloader/` — one-shot HF model puller (diffusers/Whisper/Kokoro).
- `openai-tester/` — Node integration smoke harness.

## Doing work in this repo

- **Build everything**: `./build-images.sh build`. Validates compose:
  `./build-images.sh validate`. Runs every unit test in Docker:
  `./build-images.sh test`. See [`build-images.sh`](./build-images.sh) for
  subcommands.
- **Default tag**: `v2.0.0`. Default registry: `tztcloud`. Override via
  `TAG=` and `REGISTRY=` env vars. The `-pascal` flavor of the four CUDA
  runners is the same Dockerfiles with `PYTORCH_INDEX_URL` on cu126.
- **All gestures are Docker-first.** Do not introduce steps that require host
  Python, host Go, or host Node.
- **Build context is repo root.** Every Dockerfile in `infra/dockerfiles/`
  expects to be built with `-f infra/dockerfiles/<name>.Dockerfile .`
- **Capability names are canonical.** See
  [`CANONICAL-CAPABILITIES.md`](./CANONICAL-CAPABILITIES.md). Colon form
  (`openai:chat-completions`, `text:rerank`); one value per image.

## What lives elsewhere

- **Capability broker and pool member agent.** The agent reads each runner's
  contract and relays it; the broker validates it, matches it to a catalog
  template, and dispatches paid jobs to the paths the runner declared. Both
  live in `livepeer-network-modules`; runners only see fully-authenticated
  HTTP requests at their declared endpoint.
- **Customer auth, billing, payment validation.** Handled upstream of these
  runners. The runner is blind to customer identity.
- **Video / vtuber runners.** Sibling runner families; out of scope for this repo.
