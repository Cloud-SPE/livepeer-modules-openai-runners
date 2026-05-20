# QUALITY_SCORE

Per-image grade and known gaps. Update in the same PR that changes the bar.

Grade scale:

- **A** — production-bar; gaps documented; on-call signed off.
- **B** — works end-to-end; gaps known and tracked.
- **C** — scaffold; builds; not yet exercised under real load.
- **D** — known-broken or missing critical path; do not deploy.

## Current grades (v1.3.0 — initial release)

| Image | Grade | Notes |
|---|---|---|
| `python-base` | C | Built fresh; not yet validated as a base by all consumers |
| `cuda13-python-base` | C | CUDA 13 + uv-managed Python 3.13; PyTorch via cu128 wheels |
| `openai-chat-runner` | C | Lifted Go source; multi-stage Dockerfile validated for syntax only |
| `openai-embeddings-runner` | C | Same as chat-runner |
| `openai-audio-runner` | C | Multi-stage rewrite of source Dockerfile; needs end-to-end Whisper smoke |
| `openai-tts-runner` | C | Needs Kokoro voice-mapping smoke + espeak-ng runtime presence verification |
| `openai-image-generation-runner` | C | Multi-stage; xformers + diffusers compat with cu128 on CUDA 13 unverified |
| `rerank-runner` | C | Multi-stage; sentence-transformers `<5.0.0` pin survives the rewrite |
| `image-model-downloader` | C | Simple HF puller; needs end-to-end pull validation |
| `rerank-model-downloader` | C | Same as image-model-downloader |
| `openai-tester` | C | Now 2-stage; the eight test scripts unchanged |

## Cross-cutting gaps tracked here

- **End-to-end smoke against a running broker** — none yet executed.
- **GPU runtime validation** — Dockerfiles syntax-checked only.
- **Image size measurement** — not yet recorded; should land before any grade promotes above C.
- **Reproducible builds** — no `uv.lock` files yet (see [`PLANS.md`](./PLANS.md)).
