# livepeer-modules-openai-runners

Workload binaries that serve OpenAI-shaped (and one Cohere-compatible) HTTP
endpoints to the Livepeer capability broker. One Docker image per capability;
one process per broker-dispatched container.

> **For agents:** start at [`AGENTS.md`](./AGENTS.md).

## What this repo ships

| Image | Language | Capability |
|---|---|---|
| `openai-chat-runner` | Go | `openai:chat-completions` (proxy in front of vLLM / Ollama / a hosted vendor) |
| `openai-embeddings-runner` | Go | `openai:embeddings` (proxy in front of vLLM / Ollama) |
| `openai-audio-runner` | Python | `openai:audio-transcriptions` + `openai:audio-translations` (Whisper) |
| `openai-tts-runner` | Python | `openai:audio-speech` (Kokoro TTS) |
| `openai-image-generation-runner` | Python | `openai:images-generations` (diffusers) |
| `rerank-runner` | Python | `text:rerank` (Cohere-compatible CrossEncoder) |
| `image-model-downloader` | Python | One-shot HF model puller for image-gen + audio runners |
| `rerank-model-downloader` | Python | One-shot HF model puller for rerank-runner |
| `openai-tester` | Node | Integration smoke harness across runners |

Two shared base images underpin the Python runners:

- `python-base` — Python 3.13 + `uv` on `python:3.13-slim`. Used by both downloaders.
- `cuda13-python-base` — Python 3.13 + `uv` on `nvidia/cuda:13.2.1-runtime-ubuntu24.04`.
  Used by the four ML Python runners.

## Build

Per [`CORE-BELIEFS.md`](./CORE-BELIEFS.md): every gesture is Docker-first.

```bash
./infra/scripts/build-images.sh                  # build all images (bases first)
./infra/scripts/build-images.sh audio-runner     # a subset by substring; its base is added
./infra/scripts/validate-compose.sh              # validate every compose overlay in infra/compose/
./infra/scripts/test.sh                          # go + python unit tests in Docker
PUSH=1 ./infra/scripts/build-images.sh           # build + push (clean tree only; prints digests)
./build-images.sh help                           # the root shim keeps the old subcommands
```

No host Python, host Go, or host Node required.

## Repo layout

```text
.
├── AGENTS.md, CLAUDE.md            # agent map (+ 1-liner pointer)
├── README.md, LICENSE, CHANGELOG.md
├── ARCHITECTURE.md, DESIGN.md, PLANS.md, PRODUCT_SENSE.md
├── QUALITY_SCORE.md, RELIABILITY.md, SECURITY.md
├── CORE-BELIEFS.md, BROKER-CONTRACT.md, TRUST-MODEL.md
├── CANONICAL-CAPABILITIES.md, SHARED-BASE-IMAGES.md, RUNNER-INVARIANTS.md
├── RUNNERS.md                      # per-runner sections (READMEs + runbooks)
├── build-images.sh (shim), setup-models.sh
├── infra/scripts/   # build-images.sh, validate-compose.sh, test.sh
├── infra/build/     # image-versions.env (default TAG + toolchain pins), git-version.sh, <tag>-digests.txt
├── infra/
│   ├── compose/        # 7 docker-compose overlays
│   ├── offerings/      # per-runner offering.yaml manifests
│   ├── env/            # per-runner .env.example templates
│   └── dockerfiles/    # 11 Dockerfiles, all build context = repo root
├── openai-chat-runner/, openai-embeddings-runner/   # Go source
├── openai-audio-runner/, openai-tts-runner/         # Python source
├── openai-image-generation-runner/                  # Python source
├── rerank-runner/                                   # Python source + model-downloader/
├── image-model-downloader/                          # Python source
├── openai-tester/                                   # Node source
├── docs/references/                                 # External material (point-in-time)
└── .github/workflows/                               # CI: build, release, doc-gardening
```

## The runner contract

Every image serves `GET /.well-known/livepeer-runner`: a JSON capability
entry (or an array of them) naming its capability id, transports, paths,
readiness probe, identity, and the work-unit extractor the broker should run.
The pool member agent reads it once per attach; the broker never dials a
runner. See [`BROKER-CONTRACT.md`](./BROKER-CONTRACT.md).

## Configuration

Each runner accepts common env vars (`CAPABILITY_NAME`, `DEVICE`,
`METRICS_ENABLED`, and `MODEL_ALIAS` / `SERVED_MODEL_NAME` for the identity)
plus per-capability keys. See [`RUNNERS.md`](./RUNNERS.md) for the full
per-runner list, and [`infra/env/`](./infra/env/) for copy-able `.env.example`
templates.

## Building and testing

```bash
./infra/scripts/build-images.sh          # every image at TAG (default from infra/build/image-versions.env)
./infra/scripts/validate-compose.sh      # docker compose config on every overlay
./infra/scripts/test.sh                  # go vet/test + python unittest, all in Docker
PUSH=1 ./infra/scripts/build-images.sh   # build + push; refuses a dirty tree; prints and records digests
```

Same pattern as `livepeer-network-modules/infra/scripts/build-images.sh`:
positional substring filters, `PUSH=1` to publish, `VERSION` derived from
the git tag or sha (with `-dirty` on uncommitted work) and stamped into the
Go binaries and every image's `org.opencontainers.image.version` label.
Pushed digests are appended to `infra/build/<TAG>-digests.txt`; pin those,
not the tag.

The four CUDA runners' default images install PyTorch from the cu128 wheel
index, which carries sm_75+ kernels only. For Pascal cards (sm_6x, e.g. a
GTX 1080) build the `-pascal` flavor of the audio, TTS and rerank runners.
There is no image-generation variant: FLUX.1-dev does not fit an 8 GB card
whatever the CUDA base, and the catalog does not admit it.

```bash
TAG=v2.0.0-pascal PYTORCH_INDEX_URL=https://download.pytorch.org/whl/cu126 \
  ./infra/scripts/build-images.sh openai-audio-runner openai-tts-runner rerank-runner
```

The release workflow publishes both flavors.

## Compose overlays

Per-backend compose overlays live in [`infra/compose/`](./infra/compose/):

- `docker-compose.openai-chat-runner.yml` — chat-runner sidecar.
- `docker-compose.openai-chat-runner.openai.yml` — chat-runner configured for OpenAI vendor pass-through.
- `docker-compose.openai-chat-runner.dashscope.yml` — chat-runner configured for DashScope international vendor pass-through.
- `docker-compose.openai-embeddings-runner.yml` — embeddings-runner sidecar.
- `docker-compose.vllm.chat.yml` — vLLM upstream for chat.
- `docker-compose.vllm.embeddings.yml` — vLLM upstream for embeddings.
- `docker-compose.ollama.yml` — Ollama upstream.
- `docker-compose.audio.yml` — audio + TTS overlay.
- `docker-compose.rerank-runner.yml` — rerank-runner + model downloader.

## License

MIT — see [`LICENSE`](./LICENSE).
