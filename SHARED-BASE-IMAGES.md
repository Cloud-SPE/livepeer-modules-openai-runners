# SHARED-BASE-IMAGES

Two shared base images underpin the Python tier. Go and Node runners are
self-contained (no shared bases).

**These bases are build artifacts — tagged `local/` and never pushed to a
public registry.** Only the prod runners (under `${REGISTRY}/`, default
`tztcloud/`) ship to Docker Hub.

## `local/python-base:<tag>`

| Property | Value |
|---|---|
| Dockerfile | [`infra/dockerfiles/python-base.Dockerfile`](./infra/dockerfiles/python-base.Dockerfile) |
| Upstream | `python:3.13-slim` |
| What it provides | Python 3.13 + `uv` static binary + empty `/opt/venv` + `ca-certificates` + `curl` |
| Consumers | `image-model-downloader`, `rerank-model-downloader` |
| Approximate size | ~150 MB |

Downstream Dockerfiles install their deps into `/opt/venv` via `uv pip install`.

## `local/cuda13-python-base:<tag>`

| Property | Value |
|---|---|
| Dockerfile | [`infra/dockerfiles/cuda13-python-base.Dockerfile`](./infra/dockerfiles/cuda13-python-base.Dockerfile) |
| Upstream | `nvidia/cuda:13.2.1-runtime-ubuntu24.04` |
| What it provides | CUDA 13 runtime + `uv`-managed Python 3.13 + empty `/opt/venv` + `ca-certificates` + `curl` |
| Consumers | `openai-audio-runner`, `openai-tts-runner`, `openai-image-generation-runner`, `rerank-runner` |
| Approximate size | ~3.5 GB (CUDA libraries dominate) |

Each consumer adds runtime-only apt packages in its own builder/runtime stages
(e.g. `ffmpeg` for audio + tts; `espeak-ng` for tts).

## Consumer ↔ base mapping

```text
openai-audio-runner                ─┐
openai-tts-runner                  ─┤
openai-image-generation-runner     ─┼──► cuda13-python-base
rerank-runner                      ─┘

image-model-downloader             ─┐
rerank-model-downloader            ─┴──► python-base

openai-chat-runner                  (standalone: golang:1.25.7-alpine → alpine:3.20)
openai-embeddings-runner            (standalone: golang:1.25.7-alpine → alpine:3.20)
openai-tester                       (standalone: node:22-alpine → node:22-alpine)
```

## Why two bases (and not three or one)

- **Two** — the original repo had three (CPU / GPU / GPU+media). The +media
  tier (ffmpeg pre-installed) duplicated apt logic that's better expressed
  inline in each consumer's Dockerfile. We collapsed it.
- **Not one** — the downloaders don't need the 3+ GB CUDA layer. Forcing a
  CPU-only consumer onto a CUDA base wastes pulls and storage.
- **Each consumer multi-stages on top** — so the runtime image only carries
  what it actually uses, not a kitchen-sink base.

## Adding a new base

If you find yourself adding a third base, first ask:

1. Could this be `apt-get install -y <foo>` in a single existing consumer?
2. Is the new dep used by 2+ consumers and large enough (>100 MB) that
   factoring it out is worth a new base image?

If both answers are yes, write the Dockerfile under `infra/dockerfiles/`,
add a row to the image table in [`infra/scripts/build-images.sh`](./infra/scripts/build-images.sh), and document the
consumer mapping here.

## Flavors

The four CUDA runners (`openai-audio-runner`, `openai-tts-runner`,
`openai-image-generation-runner`, `rerank-runner`) install PyTorch at build
time from `PYTORCH_INDEX_URL`. The default, cu128, ships kernels for sm_75+
only. The `-pascal` flavor (audio, TTS and rerank; image generation has none
because FLUX does not fit an 8 GB card) is the same Dockerfiles and the same base with
`PYTORCH_INDEX_URL=https://download.pytorch.org/whl/cu126` (sm_50/60/70+)
under `TAG=<version>-pascal`; the base is rebuilt under that tag too so base
and consumer never mix tags. The release workflow publishes both.
