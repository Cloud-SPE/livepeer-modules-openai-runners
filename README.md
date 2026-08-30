# livepeer-modules-openai-runners

Workload binaries that serve OpenAI-shaped (and one Cohere-compatible) HTTP
endpoints to the Livepeer capability broker. One Docker image per capability;
one process per broker-dispatched container.

> **For agents:** start at [`AGENTS.md`](./AGENTS.md).

## What this repo ships

| Image | Language | Capability |
|---|---|---|
| `openai-chat-runner` | Go | `openai-chat-completions` (proxy in front of vLLM / Ollama) |
| `openai-embeddings-runner` | Go | `openai-text-embeddings` (proxy in front of vLLM / Ollama) |
| `openai-audio-runner` | Python | `openai-audio-transcriptions` + `openai-audio-translations` (Whisper) |
| `openai-tts-runner` | Python | `openai-audio-speech` (Kokoro TTS) |
| `openai-image-generation-runner` | Python | `image-generation` (diffusers) |
| `rerank-runner` | Python | `rerank` (Cohere-compatible CrossEncoder) |
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
./build-images.sh build            # build all images (bases first)
./build-images.sh build openai-audio-runner   # build a single image
./build-images.sh validate         # validate every compose overlay in infra/compose/
./build-images.sh push             # push all to ${REGISTRY} (requires docker login)
./build-images.sh clean            # remove locally-built images
./build-images.sh help             # show all subcommands
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
├── build-images.sh, setup-models.sh
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

## Configuration

Each runner accepts common env vars (`CAPABILITY_NAME`, `DEVICE`,
`METRICS_ENABLED`) plus per-capability keys. See [`RUNNERS.md`](./RUNNERS.md)
for the full per-runner list, and [`infra/env/`](./infra/env/) for copy-able
`.env.example` templates.

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
