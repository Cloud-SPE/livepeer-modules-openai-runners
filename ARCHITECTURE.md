# ARCHITECTURE

Top-level domain map. Read [`DESIGN.md`](./DESIGN.md) for the one-page mental
model; read this for the structural picture.

## Position in the broader system

This repo is the **workload-binary tier** for OpenAI-shaped and
Cohere-compatible capabilities. It sits behind the capability broker — broker
is the client, runner is the server. The broker handles auth, billing,
payment validation, and dispatch; the runner receives a fully-authenticated
HTTP request and does the work.

```text
   capability-broker (orch host)
       │ Livepeer-Mode dispatch
       │ POST /v1/cap → forwards to a configured backend per host-config.yaml
       │
       ├──► /v1/chat/completions      → openai-chat-runner       → Ollama / vLLM upstream
       ├──► /v1/embeddings            → openai-embeddings-runner → Ollama / vLLM upstream
       ├──► /v1/audio/transcriptions  → openai-audio-runner       (Whisper)
       ├──► /v1/audio/translations    → openai-audio-runner       (Whisper)
       ├──► /v1/audio/speech          → openai-tts-runner         (Kokoro)
       ├──► /v1/images/generations    → openai-image-generation-runner (diffusers)
       └──► /v1/rerank                → rerank-runner             (CrossEncoder)
```

## Layered model

Within a runner, code is organized into a small fixed set of layers (Python
runners; the Go proxies are simpler):

- **Entrypoint** (`__main__.py`, `cmd/runner/main.go`) — wire env → service.
- **Service** (`app.py`, `internal/runner/runner.go`) — HTTP handlers; queue
  management; usage reporting.
- **Loader** (`whisper_loader.py`, `kokoro_loader.py`, `diffusers_loader.py`,
  `model_loader.py`) — model load, warm-up, inference.
- **Probe** (`gpu_probe.py`) — startup-time GPU validation.

Cross-cutting concerns enter through explicit configuration:

- **Capability identity**: `CAPABILITY_NAME` env var (one value per image).
- **Device**: `DEVICE=cuda|cpu` with fail-fast on cuda + no GPU.
- **Metrics**: `METRICS_ENABLED=true` exposes `/metrics`.
- **Offering**: `/etc/runner/offering.yaml` baked into each image at build
  time; provides the broker with default options + rate-card hints.

## Image hierarchy

Two shared bases ([`SHARED-BASE-IMAGES.md`](./SHARED-BASE-IMAGES.md)):

```text
python:3.13-slim                                python-base                   ← cpu runners
   └── + uv + ca-certs/curl                          ↑
                                                     ├── image-model-downloader
                                                     └── rerank-model-downloader

nvidia/cuda:13.2.1-runtime-ubuntu24.04          cuda13-python-base            ← gpu runners
   └── + uv-managed Python 3.13                      ↑
                                                     ├── openai-audio-runner       (+ ffmpeg)
                                                     ├── openai-tts-runner         (+ ffmpeg, espeak-ng)
                                                     ├── openai-image-generation-runner
                                                     └── rerank-runner

# Standalone (no shared base):
openai-chat-runner          (golang:1.25.7-alpine → alpine:3.20)
openai-embeddings-runner    (golang:1.25.7-alpine → alpine:3.20)
openai-tester               (node:22-alpine → node:22-alpine, 2-stage)
```

## Build context contract

Every Dockerfile in `infra/dockerfiles/` is built with **build context = repo
root**. This is what lets a Dockerfile COPY from `infra/offerings/` and from
its runner's source directory in the same image without volume mounts.

## What's deliberately out of scope

- **Customer auth, billing, payment validation.** Lives upstream of the broker.
- **Capability registration.** The orch-coordinator scrapes
  `GET /<capability>/options` from each running runner.
- **Mode dispatch + extractor logic.** Lives in the capability broker.
- **Wire-protocol middleware.** Lives in the gateway tier upstream of the broker.
- **Video / vtuber runners.** Sibling repos.
