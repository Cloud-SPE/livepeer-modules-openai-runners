# Changelog

All notable changes to this repo. Format roughly follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [v2.0.0] — 2026-09-04

**Breaking.** Every image now serves the runner contract and the old
`/options` discovery surface is gone; default capability ids change form.
A broker on the pre-contract paradigm cannot use these images, and a
pool member agent older than `livepeer-network-modules` 3686b67 cannot
read the audio runner's two-entry contract.

- **Runner contract.** Every image serves `GET /.well-known/livepeer-runner`
  (livepeer-network-protocol `runner-contract.md` 1.1.0): the runner-owned
  half of a capability entry, relayed by the pool member agent. `GET
  /<capability>/options` is removed on all six runners; nothing reads it.
  `BROKER-CONTRACT.md` is rewritten against the contract.
- **Capability ids move to the catalog's colon form** (overridable via
  `CAPABILITY_NAME`): `openai:chat-completions`, `openai:embeddings`,
  `openai:audio-transcriptions` + `openai:audio-translations`,
  `openai:audio-speech`, `openai:images-generations`, `text:rerank`.
- **Audio runner declares two capabilities** as a JSON array from one port.
  `CAPABILITY_NAME` unset serves both; set to one of the two ids serves
  that entry only (the pool-host shape).
- **Identity.** Chat and embeddings advertise `identity.openai.model` per
  served model (`SERVED_MODEL_NAME`, else one entry per discovered model —
  an array when several). Audio/TTS/rerank take a `MODEL_ALIAS`
  (`whisper-large-v3`, `kokoro`, `zerank-2`); rerank uses the plain
  `identity.model` key because it is not an OpenAI endpoint. The rerank
  response `meta.model` is now the alias, with `meta.backend_model` the HF id.
- **Work units are declared, not configured.** Chat and embeddings declare
  `openai-usage`; audio and rerank declare `response-header` on
  `X-Livepeer-Work-Units`; TTS and image generation declare
  `request-formula` (input characters; `n` images). The rerank runner now
  emits `X-Livepeer-Work-Units: <documents scored>` on every success.
  The chat runner's `OUTPUT_TOKEN_WEIGHT` header/trailer stays
  informational: the broker bills from the body.
- **`GET /v1/models`** on the Go proxies (OpenAI list shape, after
  `MODEL_ALLOWLIST`) backs the `http-openai-model-ready` readiness probe.
- **GPU probe checks the architecture.** Python runners exit non-zero at
  startup when the device's compute capability is not in the torch build's
  arch list, naming the fix. The default cu128 wheels ship sm_75+ only;
  a **`-pascal` image flavor** (cu126 wheels, `v2.0.0-pascal`) covers
  sm_6x cards such as the GTX 1080 and is published by the release workflow.
- **CI runs tests.** `./build-images.sh test` runs `go vet`/`go test` for
  both Go modules and Python `unittest` for the contract and GPU-probe
  modules, all in Docker; the build workflow gates on it.
- `infra/env/*.env.example` are now tracked (they were gitignored by `env/`).
- **Build system moves under `infra/`**, following
  `livepeer-network-modules/infra/scripts/build-images.sh`: one image table
  with base-image dependencies resolved automatically, substring filters,
  `PUSH=1` that refuses a dirty tree and prints + records pushed digests
  (`infra/build/<TAG>-digests.txt`), `VERSION` derived from git and stamped
  into the Go binaries and every image's OCI version label, default tag and
  toolchain pins in `infra/build/image-versions.env`. `validate-compose.sh`
  and `test.sh` are separate scripts; the root `build-images.sh` is a shim
  for the old subcommands. The release workflow builds and pushes in one
  pass and attaches the digest records to the GitHub release.
- `openai-chat-runner`: `UPSTREAM_KIND=ollama` is accepted again alongside
  `vllm`, `openai`, and `dashscope`; the vendor work had dropped it.

### Carried from the unreleased vendor pass-through work

- **Phase 0 vendor pass-through**: `openai-chat-runner` now supports bounded
  OpenAI and DashScope upstream kinds, operator-supplied outbound bearer auth,
  exact model allowlisting, checked per-model output-token weighting, sanitized
  refundable vendor failures, dedicated vendor compose overlays, and a
  deterministic authenticated vendor integration fixture in `openai-tester`.

## [v1.3.0] — 2026-05-19

Initial release of the standalone runner repo. Includes:

- **OpenAI-shaped runners**: `openai-chat-runner` (Go), `openai-embeddings-runner` (Go),
  `openai-audio-runner` (Python — Whisper STT), `openai-tts-runner` (Python — Kokoro TTS),
  `openai-image-generation-runner` (Python — diffusers).
- **Cohere-compatible runner**: `rerank-runner` (Python — sentence-transformers CrossEncoder).
- **Helpers**: `image-model-downloader`, `rerank-model-downloader`, `openai-tester` (Node smoke harness).
- **Shared bases**: `python-base` (CPU; Python 3.13) and `cuda13-python-base` (CUDA 13 + Python 3.13).
- **Build orchestration**: single `build-images.sh` at repo root.
- **Infra layout**: docker-compose overlays, offering manifests, and env templates centralized under `infra/`.
- **CI**: GitHub Actions workflows for PR builds, tagged releases, and weekly doc-gardening.

[v1.3.0]: https://github.com/Cloud-SPE/livepeer-modules-openai-runners/releases/tag/v1.3.0
